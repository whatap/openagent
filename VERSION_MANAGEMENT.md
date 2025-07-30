# OpenAgent Version Management System

## 📋 Overview

This document describes the comprehensive version management system implemented for the OpenAgent project. The system provides centralized version control, build history tracking, and semantic versioning support.

## 🎯 Features

- **Centralized Version Management**: Single source of truth in `version.properties`
- **Semantic Versioning**: Support for MAJOR.MINOR.PATCH versioning
- **Build History Tracking**: Complete build history with metadata in `build-history.json`
- **Git Integration**: Automatic git information capture
- **Backward Compatibility**: Maintains support for manual version specification

## 📁 Files

### Core Files
- `version.properties` - Central version configuration file
- `build-history.json` - Build history and statistics
- `version.sh` - Version management utility script
- `build.sh` - Enhanced build script with version management

## 🚀 Usage Guide

### Version Management Commands

#### Show Current Version
```bash
./version.sh show
# Output: Current version: 1.0.1
```

#### Increment Version
```bash
# Increment patch version (1.0.0 -> 1.0.1)
./version.sh increment patch

# Increment minor version (1.0.1 -> 1.1.0)
./version.sh increment minor

# Increment major version (1.1.0 -> 2.0.0)
./version.sh increment major
```

### Build Commands

#### Traditional Usage (Manual Version)
```bash
# Specify version manually
./build.sh 1.2.3 arm64
./build.sh 1.2.3 all docker.io/myuser
```

#### Version File Usage
```bash
# Read version from version.properties
./build.sh --from-file
./build.sh --from-file arm64
./build.sh --from-file all docker.io/myuser
```

#### Auto-Increment and Build
```bash
# Increment patch version and build
./build.sh --increment-patch

# Increment minor version and build for specific architecture
./build.sh --increment-minor arm64

# Increment major version and build with custom registry
./build.sh --increment-major all docker.io/myuser
```

## 📊 Build History

### Viewing Build History
```bash
# View recent builds (requires jq)
jq '.builds[-5:]' build-history.json

# View build statistics
jq '.statistics' build-history.json

# View builds for specific version
jq '.builds[] | select(.version == "1.0.1")' build-history.json
```

### Build History Structure
Each build entry contains:
- Build ID and timestamp
- Version and version source
- Architecture and platforms
- Git information (commit, branch, tag)
- Build metadata (user, host, duration)
- Docker images created
- S3 upload information
- Build status and notes

## 🔧 Configuration

### version.properties Structure
```properties
# Current version
VERSION=1.0.1

# Version components
MAJOR=1
MINOR=0
PATCH=1

# Build metadata
BUILD_NUMBER=2
RELEASE_DATE=2025-01-30
RELEASE_NOTES=Updated version management system

# Git information (auto-updated)
GIT_COMMIT=abc1234
GIT_BRANCH=main
GIT_TAG=

# Build information (auto-updated)
LAST_BUILD_DATE=2025-01-30
LAST_BUILD_USER=developer
LAST_BUILD_HOST=build-server
```

## 🔄 Workflow Examples

### Development Workflow
```bash
# 1. Make code changes
git add .
git commit -m "Add new feature"

# 2. Increment patch version and build
./build.sh --increment-patch

# 3. View build history
jq '.builds[-1]' build-history.json
```

### Release Workflow
```bash
# 1. Increment minor version for new features
./build.sh --increment-minor

# 2. Tag the release
git tag v$(./version.sh show | cut -d' ' -f3)
git push origin --tags

# 3. Build and deploy
./build.sh --from-file all
```

### Hotfix Workflow
```bash
# 1. Create hotfix branch
git checkout -b hotfix/critical-fix

# 2. Make fixes and increment patch
./build.sh --increment-patch

# 3. Merge and deploy
git checkout main
git merge hotfix/critical-fix
```

## 📈 Version History Tracking

The system automatically maintains version history in the `version.properties` file:

```properties
# Version history (recent versions)
HISTORY_1=1.0.0|2025-01-30|Initial version
HISTORY_2=1.0.1|2025-01-30|Bug fixes and improvements
```

## 🛠️ Advanced Features

### Custom Version Notes
You can add custom release notes by editing the `RELEASE_NOTES` field in `version.properties` before building.

### Build Metadata
The system automatically captures:
- Git commit hash and branch
- Build user and hostname
- Build duration and timestamp
- Docker images created
- Architecture and platforms built

### Integration with CI/CD
The version management system is designed to work with CI/CD pipelines:

```bash
# In CI/CD pipeline
if [ "$BRANCH" = "main" ]; then
    ./build.sh --increment-minor
elif [ "$BRANCH" = "develop" ]; then
    ./build.sh --increment-patch
else
    ./build.sh --from-file
fi
```

## 🔍 Troubleshooting

### Common Issues

#### jq Not Found
If `jq` is not installed, build history tracking will be disabled:
```bash
# Install jq on macOS
brew install jq

# Install jq on Ubuntu/Debian
sudo apt-get install jq
```

#### Version File Not Found
If `version.properties` doesn't exist, the system will default to version 1.0.0.

#### Git Information Missing
If not in a git repository, git fields will be empty but the system will continue to work.

## 📋 Best Practices

1. **Always use version management options** instead of manual versions for consistency
2. **Review build history** regularly to track deployment patterns
3. **Use semantic versioning** appropriately (major for breaking changes, minor for features, patch for fixes)
4. **Tag releases** in git to match version numbers
5. **Document significant changes** in the RELEASE_NOTES field

## 🎯 Migration from Manual Versioning

If you're migrating from manual version specification:

1. **Current builds** continue to work with manual version specification
2. **Gradually adopt** version file usage with `--from-file`
3. **Use increment options** for new development
4. **Review and update** version.properties as needed

The system maintains full backward compatibility while providing enhanced version management capabilities.

## ✅ Status

- ✅ Version file management
- ✅ Semantic versioning support
- ✅ Build history tracking
- ✅ Git integration
- ✅ Backward compatibility
- ✅ Comprehensive documentation

The version management system is fully implemented and ready for production use!