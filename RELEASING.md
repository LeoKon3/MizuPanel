# Release Versioning

## Version bump policy

Every user-facing bug-fix or feature delivery batch increments the SemVer patch
version exactly once before handoff. Multiple commits that belong to the same
delivery batch share that one bump. Documentation-only, test-only, and internal
maintenance changes do not require a version bump unless explicitly requested.

## Synchronized version surfaces

Update all of these files to the same version:

- `VERSION`
- `internal/version/version.go`
- `web/package.json`
- the top-level and root-package versions in `web/package-lock.json`
- the version badges in `README.md` and `README.en.md`
- the newest `CHANGELOG.md` release heading

Add release notes as a new topmost `## v<version> - YYYY-MM-DD` section. Never
rename a historical release section to describe new work.

## Verification

Run the synchronization guard before building release artifacts:

```bash
go test ./internal/version
```

Then rebuild the frontend, Server, local Agent, and downloadable Agent binaries.
Release tags must be exactly `v<version>`; the GitHub release workflow rejects a
tag that does not match `VERSION`.
