#!/usr/bin/env bash
set -euo pipefail

# Version Management Utility for OpenAgent
# Usage: ./version.sh [command] [options]

VERSION_FILE="version.properties"
HISTORY_FILE="build-history.json"

# Read version from properties file
get_version() {
    if [[ ! -f "$VERSION_FILE" ]]; then
        echo "1.0.0"
        return
    fi
    grep "^VERSION=" "$VERSION_FILE" | cut -d'=' -f2
}

# Show current version
show_version() {
    echo "Current version: $(get_version)"
}

# Update version in properties file
update_version_file() {
    local new_version=$1
    local major minor patch
    
    IFS='.' read -r major minor patch <<< "$new_version"
    
    # Create backup
    cp "$VERSION_FILE" "${VERSION_FILE}.bak"
    
    # Update version components
    sed -i.tmp "s/^VERSION=.*/VERSION=$new_version/" "$VERSION_FILE"
    sed -i.tmp "s/^MAJOR=.*/MAJOR=$major/" "$VERSION_FILE"
    sed -i.tmp "s/^MINOR=.*/MINOR=$minor/" "$VERSION_FILE"
    sed -i.tmp "s/^PATCH=.*/PATCH=$patch/" "$VERSION_FILE"
    
    # Update build metadata
    local current_date=$(date -u +"%Y-%m-%d")
    local current_user=$(whoami)
    local current_host=$(hostname)
    
    sed -i.tmp "s/^LAST_BUILD_DATE=.*/LAST_BUILD_DATE=$current_date/" "$VERSION_FILE"
    sed -i.tmp "s/^LAST_BUILD_USER=.*/LAST_BUILD_USER=$current_user/" "$VERSION_FILE"
    sed -i.tmp "s/^LAST_BUILD_HOST=.*/LAST_BUILD_HOST=$current_host/" "$VERSION_FILE"
    
    # Update git information if available
    if command -v git &> /dev/null && git rev-parse --git-dir > /dev/null 2>&1; then
        local git_commit=$(git rev-parse --short HEAD 2>/dev/null || echo "")
        local git_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
        local git_tag=$(git describe --tags --exact-match 2>/dev/null || echo "")
        
        sed -i.tmp "s/^GIT_COMMIT=.*/GIT_COMMIT=$git_commit/" "$VERSION_FILE"
        sed -i.tmp "s/^GIT_BRANCH=.*/GIT_BRANCH=$git_branch/" "$VERSION_FILE"
        sed -i.tmp "s/^GIT_TAG=.*/GIT_TAG=$git_tag/" "$VERSION_FILE"
    fi
    
    # Clean up temporary files
    rm -f "${VERSION_FILE}.tmp"
    
    echo "✅ Updated version to $new_version"
}

# Increment version
increment_version() {
    local type=$1
    local current=$(get_version)
    local major minor patch
    
    IFS='.' read -r major minor patch <<< "$current"
    
    case $type in
        major)
            major=$((major + 1))
            minor=0
            patch=0
            ;;
        minor)
            minor=$((minor + 1))
            patch=0
            ;;
        patch)
            patch=$((patch + 1))
            ;;
        *)
            echo "Invalid increment type: $type"
            exit 1
            ;;
    esac
    
    local new_version="${major}.${minor}.${patch}"
    
    echo "🔄 Incrementing version from $current to $new_version ($type)"
    update_version_file "$new_version"
    
    echo "$new_version"
}

# Show usage
show_usage() {
    echo "Usage: $0 [command]"
    echo "Commands:"
    echo "  show                 Show current version"
    echo "  increment <type>     Increment version (major|minor|patch)"
    echo "  help                 Show this help"
}

case "${1:-help}" in
    show)
        show_version
        ;;
    increment)
        if [[ $# -lt 2 ]]; then
            echo "Error: increment requires type (major|minor|patch)"
            exit 1
        fi
        increment_version "$2"
        ;;
    help|--help|-h)
        show_usage
        ;;
    *)
        echo "Unknown command: $1"
        show_usage
        exit 1
        ;;
esac