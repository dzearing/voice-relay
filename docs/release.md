# Release Policy

## Versioning

We use [Semantic Versioning](https://semver.org/): `vMAJOR.MINOR.PATCH`

- **PATCH** (`v1.5.1`): Bug fixes, small tweaks
- **MINOR** (`v1.6.0`): New features, UI changes, non-breaking improvements
- **MAJOR** (`v2.0.0`): Breaking changes, major architecture shifts

## Release Steps

1. **Ensure all changes are committed and pushed to `main`.**

2. **Check the latest tag:**
   ```bash
   git tag --sort=-v:refname | head -5
   ```

3. **Determine the new version** based on the changes (see Versioning above).

4. **Create the release with `gh`:**
   ```bash
   gh release create v<VERSION> --target main --title "v<VERSION>" --notes "<RELEASE_NOTES>"
   ```

5. **Release notes format:**
   ```
   ## What's New

   ### Feature Name
   - Bullet points describing the feature

   ### Bug Fixes
   - Bullet points describing fixes
   ```

## Config Versioning

The app config (`config.yaml`) has its own version tracked by `CurrentConfigVersion` in `internal/config/config.go`. When the config schema changes in a way that requires a fresh start (new required fields, renamed keys, changed defaults), bump `CurrentConfigVersion`. On startup, if the saved config version is lower than the current version, the config file is deleted and regenerated with new defaults.

This means users don't need to manually edit their config after an upgrade — the app handles it automatically.

## Notes

- Releases are created directly from `main` — no release branches.
- Tags are created automatically by `gh release create`.
- Do not tag manually; let `gh` handle it to keep tags and releases in sync.
