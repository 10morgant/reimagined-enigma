# reimagined-enigma

## Project audit CLI (Go)

`audit.ps1` has a Go equivalent at `cmd/project-audit`.

### Build

```bash
go build ./cmd/project-audit
```

### Usage

```bash
go run ./cmd/project-audit \
  -path "/path/to/projects" \
  -age 2y \
  -filter-by Oldest \
  -output "./ProjectAudit_20260507_193000.csv" \
  -output-format confluence
```

Flags:

- `-path` (required): root projects folder.
- `-age`: cutoff in `<number><unit>` format (`d`, `w`, `m`, `y`), default `2y`.
- `-filter-by`: `Created`, `Modified`, or `Oldest`, default `Oldest`.
- `-output`: output path, default `ProjectAudit_<timestamp>.csv`.
- `-output-format`: `csv` or `confluence`, default `confluence`.
- `-walk-workers`: project directory walk workers (default `NumCPU`).
- `-meta-workers`: file metadata workers (default `NumCPU`).

Behavior matches the script:

- Scans top-level project directories.
- If `Archived Projects` exists, scans each immediate subfolder under it as `[Archived] <name>`.
- Recursively enumerates files per project.
- Reports file name, full path, project label, owner (best effort), size, created, last modified, age, and days since modified.
- Supports CSV and Confluence wiki table output.
- Shows thread-safe progress while workers are running.

### Platform notes

- **File owner**: best effort. On Unix-like systems, owner is resolved from file UID; on Windows it falls back to `Unknown (owner lookup unavailable)`.
- **Created time**: on Windows this uses file creation time. On Unix-like systems creation time is not consistently portable, so created time falls back to file modification time.
