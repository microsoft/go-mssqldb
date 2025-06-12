# Copilot Instructions: Go Version Upgrades

This document provides instructions for maintaining the AppVeyor and GitHub Actions workflow files when upgrading the Go version for the go-mssqldb project.

## Files to Update

When upgrading Go versions, the following files need to be updated:

1. **`.github/workflows/pr-validation.yml`** - GitHub Actions workflow for pull request validation
2. **`appveyor.yml`** - AppVeyor configuration for Windows testing

## GitHub Actions Workflow (.github/workflows/pr-validation.yml)

### What to Update

In the `pr-validation.yml` file, update the Go version in the strategy matrix:

```yaml
strategy:
  matrix:
    go: ['1.XX']  # Update this version number
    sqlImage: ['2019-latest','2022-latest']
```

### Example

When upgrading from Go 1.22 to Go 1.23:

```yaml
# Before
go: ['1.22']

# After  
go: ['1.23']
```

For major version upgrades, you may want to test multiple versions temporarily:

```yaml
# Testing multiple versions during transition
go: ['1.22', '1.23']
```

## AppVeyor Configuration (appveyor.yml)

### What to Update

The AppVeyor configuration requires updating multiple `GOVERSION` entries:

1. **Default GOVERSION** (line ~14):
   ```yaml
   GOVERSION: 123  # Update to new version (remove dots)
   ```

2. **Matrix entries** (lines ~20-35):
   ```yaml
   matrix:
     - SQLINSTANCE: SQL2017
     - GOVERSION: 123              # Update this
       SQLINSTANCE: SQL2017
     - GOVERSION: 123              # Update this
       SQLINSTANCE: SQL2019
       COLUMNENCRYPTION: 1
     - GOVERSION: 123-x86          # Update this (keep -x86 suffix)
       SQLINSTANCE: SQL2017
       GOARCH: 386
       PROTOCOL: np
       TAGS: -tags np
     - GOVERSION: 123              # Update this
       SQLINSTANCE: SQL2019
       PROTOCOL: lpc
       TAGS: -tags sm
   ```

### Version Format

AppVeyor uses a different version format than GitHub Actions:
- **GitHub Actions**: Use full version with dots (e.g., `'1.23'`)
- **AppVeyor**: Remove dots and use just numbers (e.g., `123` for Go 1.23)

### Example

When upgrading from Go 1.22 to Go 1.23:

```yaml
# Before
GOVERSION: 122

# After
GOVERSION: 123
```

For the x86 variant:
```yaml
# Before  
GOVERSION: 122-x86

# After
GOVERSION: 123-x86
```

## Complete Upgrade Checklist

When upgrading Go versions:

- [ ] Update `.github/workflows/pr-validation.yml`:
  - [ ] Update the `go` version in the matrix strategy
- [ ] Update `appveyor.yml`:
  - [ ] Update the default `GOVERSION` environment variable
  - [ ] Update all `GOVERSION` entries in the matrix (typically 4 locations)
  - [ ] Ensure the x86 version maintains its `-x86` suffix
- [ ] Test the changes:
  - [ ] Verify GitHub Actions workflow runs successfully
  - [ ] Verify AppVeyor builds complete successfully
- [ ] Consider updating `go.mod` if using a newer Go version than currently required

## Notes

- **Always update all instances**: Missing a GOVERSION update in the AppVeyor matrix will result in mixed Go versions in CI
- **Version consistency**: Keep all Go versions consistent across both workflow files
- **Testing strategy**: Consider running a test build before merging to ensure the new Go version works correctly with the project dependencies
- **Backward compatibility**: When upgrading Go versions, ensure the new version doesn't break existing functionality or dependencies

## Historical Context

Previous Go version upgrades have typically involved:
- Updating from multiple Go versions (e.g., 1.19, 1.20) to a single latest version (e.g., 1.23)
- Removing deprecated parameters like `RACE` settings when they're no longer needed
- Maintaining support for both Linux (GitHub Actions) and Windows (AppVeyor) environments