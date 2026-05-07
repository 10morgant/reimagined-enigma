package audit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type FilterMode string

const (
	FilterCreated  FilterMode = "Created"
	FilterModified FilterMode = "Modified"
	FilterOldest   FilterMode = "Oldest"
)

type OutputFormat string

const (
	OutputCSV        OutputFormat = "csv"
	OutputConfluence OutputFormat = "confluence"
)

type Config struct {
	RootPath     string
	Age          string
	FilterBy     FilterMode
	OutputPath   string
	OutputFormat OutputFormat
	WalkWorkers  int
	MetaWorkers  int
	Now          func() time.Time
}

type ProjectRoot struct {
	Label string
	Path  string
}

type FileTask struct {
	Path         string
	ProjectLabel string
}

type Row struct {
	FileName          string
	FullPath          string
	ProjectFolder     string
	Owner             string
	Size              string
	Created           string
	LastModified      string
	Age               string
	DaysSinceModified string
}

type Progress struct {
	projectsTotal  int64
	projectsDone   int64
	filesFound     int64
	filesProcessed int64
	filesMatched   int64
}

func (p *Progress) SetProjectsTotal(n int) {
	atomic.StoreInt64(&p.projectsTotal, int64(n))
}

func (p *Progress) IncProjectsDone() {
	atomic.AddInt64(&p.projectsDone, 1)
}

func (p *Progress) IncFilesFound() {
	atomic.AddInt64(&p.filesFound, 1)
}

func (p *Progress) IncFilesProcessed() {
	atomic.AddInt64(&p.filesProcessed, 1)
}

func (p *Progress) IncFilesMatched() {
	atomic.AddInt64(&p.filesMatched, 1)
}

func (p *Progress) Snapshot() (projectsDone, projectsTotal, filesFound, filesProcessed, filesMatched int64) {
	return atomic.LoadInt64(&p.projectsDone), atomic.LoadInt64(&p.projectsTotal), atomic.LoadInt64(&p.filesFound), atomic.LoadInt64(&p.filesProcessed), atomic.LoadInt64(&p.filesMatched)
}

func ParseAge(age string, now time.Time) (time.Time, error) {
	if len(age) < 2 {
		return time.Time{}, fmt.Errorf("invalid age format %q, expected <number><unit>", age)
	}

	unit := age[len(age)-1]
	valuePart := age[:len(age)-1]
	var value int
	if _, err := fmt.Sscanf(valuePart, "%d", &value); err != nil || value < 0 {
		return time.Time{}, fmt.Errorf("invalid age value %q", age)
	}

	switch unit {
	case 'd':
		return now.AddDate(0, 0, -value), nil
	case 'w':
		return now.AddDate(0, 0, -(value * 7)), nil
	case 'm':
		return now.AddDate(0, -value, 0), nil
	case 'y':
		return now.AddDate(-value, 0, 0), nil
	default:
		return time.Time{}, fmt.Errorf("invalid age unit %q, expected d/w/m/y", string(unit))
	}
}

func FormatAge(span time.Duration) string {
	daysTotal := int(span.Hours() / 24)
	if daysTotal <= 0 {
		return "0d"
	}

	years := daysTotal / 365
	remain := daysTotal % 365
	months := remain / 30
	days := remain % 30

	parts := make([]string, 0, 3)
	if years > 0 {
		parts = append(parts, fmt.Sprintf("%dy", years))
	}
	if months > 0 {
		parts = append(parts, fmt.Sprintf("%dm", months))
	}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if len(parts) == 0 {
		return "0d"
	}
	return strings.Join(parts, " ")
}

func FormatSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
		tb = 1024 * gb
	)

	switch {
	case bytes < kb:
		return fmt.Sprintf("%d B", bytes)
	case bytes < mb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/kb)
	case bytes < gb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/mb)
	case bytes < tb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/gb)
	default:
		return fmt.Sprintf("%.1f TB", float64(bytes)/tb)
	}
}

func ValidateFilter(mode FilterMode) error {
	switch mode {
	case FilterCreated, FilterModified, FilterOldest:
		return nil
	default:
		return fmt.Errorf("unsupported filter mode %q", mode)
	}
}

func DiscoverProjectRoots(rootPath string) ([]ProjectRoot, error) {
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return nil, err
	}

	projects := make([]ProjectRoot, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		full := filepath.Join(rootPath, entry.Name())
		if strings.EqualFold(entry.Name(), "Archived Projects") {
			archivedEntries, err := os.ReadDir(full)
			if err != nil {
				continue
			}
			for _, archived := range archivedEntries {
				if !archived.IsDir() {
					continue
				}
				projects = append(projects, ProjectRoot{
					Label: "[Archived] " + archived.Name(),
					Path:  filepath.Join(full, archived.Name()),
				})
			}
			continue
		}
		projects = append(projects, ProjectRoot{Label: entry.Name(), Path: full})
	}

	sort.Slice(projects, func(i, j int) bool {
		return strings.ToLower(projects[i].Label) < strings.ToLower(projects[j].Label)
	})
	return projects, nil
}

func Run(ctx context.Context, cfg Config, progress *Progress) ([]Row, error) {
	if err := ValidateFilter(cfg.FilterBy); err != nil {
		return nil, err
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.WalkWorkers <= 0 {
		cfg.WalkWorkers = runtime.NumCPU()
	}
	if cfg.MetaWorkers <= 0 {
		cfg.MetaWorkers = runtime.NumCPU()
	}

	now := cfg.Now()
	cutoff, err := ParseAge(cfg.Age, now)
	if err != nil {
		return nil, err
	}

	projectRoots, err := DiscoverProjectRoots(cfg.RootPath)
	if err != nil {
		return nil, err
	}
	if len(projectRoots) == 0 {
		return nil, errors.New("no project folders found")
	}
	progress.SetProjectsTotal(len(projectRoots))

	projectCh := make(chan ProjectRoot)
	fileCh := make(chan FileTask, cfg.MetaWorkers*4)
	resultsCh := make(chan Row, cfg.MetaWorkers*4)
	errCh := make(chan error, 1)

	var walkWG sync.WaitGroup
	for i := 0; i < cfg.WalkWorkers; i++ {
		walkWG.Add(1)
		go func() {
			defer walkWG.Done()
			for project := range projectCh {
				err := filepath.WalkDir(project.Path, func(path string, d fs.DirEntry, walkErr error) error {
					if walkErr != nil {
						return nil
					}
					if d.IsDir() {
						return nil
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					case fileCh <- FileTask{Path: path, ProjectLabel: project.Label}:
						progress.IncFilesFound()
						return nil
					}
				})
				if err != nil && !errors.Is(err, context.Canceled) {
					select {
					case errCh <- err:
					default:
					}
				}
				progress.IncProjectsDone()
			}
		}()
	}

	var metaWG sync.WaitGroup
	for i := 0; i < cfg.MetaWorkers; i++ {
		metaWG.Add(1)
		go func() {
			defer metaWG.Done()
			for task := range fileCh {
				row, ok := buildRow(task, cfg.FilterBy, cutoff, now)
				progress.IncFilesProcessed()
				if !ok {
					continue
				}
				progress.IncFilesMatched()
				select {
				case <-ctx.Done():
					return
				case resultsCh <- row:
				}
			}
		}()
	}

	go func() {
		defer close(projectCh)
		for _, project := range projectRoots {
			select {
			case <-ctx.Done():
				return
			case projectCh <- project:
			}
		}
	}()

	go func() {
		walkWG.Wait()
		close(fileCh)
	}()

	go func() {
		metaWG.Wait()
		close(resultsCh)
	}()

	rows := make([]Row, 0, 1024)
	for {
		select {
		case err := <-errCh:
			if err != nil {
				return nil, err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		case row, ok := <-resultsCh:
			if !ok {
				sort.Slice(rows, func(i, j int) bool {
					if rows[i].ProjectFolder == rows[j].ProjectFolder {
						return rows[i].FullPath < rows[j].FullPath
					}
					return rows[i].ProjectFolder < rows[j].ProjectFolder
				})
				return rows, nil
			}
			rows = append(rows, row)
		}
	}
}

func buildRow(task FileTask, filterBy FilterMode, cutoff time.Time, now time.Time) (Row, bool) {
	fi, err := os.Stat(task.Path)
	if err != nil {
		return Row{}, false
	}
	created := createdTime(fi)
	modified := fi.ModTime()
	filterDate := selectFilterDate(created, modified, filterBy)
	if !filterDate.Before(cutoff) {
		return Row{}, false
	}

	oldest := modified
	if created.Before(modified) {
		oldest = created
	}

	return Row{
		FileName:          fi.Name(),
		FullPath:          task.Path,
		ProjectFolder:     task.ProjectLabel,
		Owner:             fileOwner(fi),
		Size:              FormatSize(fi.Size()),
		Created:           created.Format("2006-01-02 15:04:05"),
		LastModified:      modified.Format("2006-01-02 15:04:05"),
		Age:               FormatAge(now.Sub(oldest)),
		DaysSinceModified: FormatAge(now.Sub(modified)),
	}, true
}

func selectFilterDate(created time.Time, modified time.Time, mode FilterMode) time.Time {
	switch mode {
	case FilterCreated:
		return created
	case FilterModified:
		return modified
	case FilterOldest:
		fallthrough
	default:
		if created.Before(modified) {
			return created
		}
		return modified
	}
}
