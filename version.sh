#!/usr/bin/env bash
set -euo pipefail

if [[ -n ${GIT_COMMIT-} ]] || GIT_COMMIT=$(git rev-parse "HEAD^{commit}" 2>/dev/null); then
  if [[ -z ${GIT_TREE_STATE-} ]]; then
    # Check if the tree is dirty.  default to dirty
    if git_status=$(git status --porcelain 2>/dev/null) && [[ -z ${git_status} ]]; then
      GIT_TREE_STATE="clean"
    else
      GIT_TREE_STATE="dirty"
    fi
  fi

  # Use git describe to find the version based on tags.
  if [[ -n ${GIT_VERSION-} ]] || GIT_VERSION=$(git describe --tags --abbrev=14 "${GIT_COMMIT}^{commit}" 2>/dev/null); then
    # This translates the "git describe" to an actual semver.org
    # compatible semantic version that looks something like this:
    #   v1.0.0-beta.0.10+4c183422345d8f
    #
    # downstream consumers are expecting it there.
    DASHES_IN_VERSION=$(echo "${GIT_VERSION}" | sed "s/[^-]//g")
    if [[ "${DASHES_IN_VERSION}" == "---" ]] ; then
      # We have distance to subversion (v1.1.0-subversion-1-gCommitHash)
      GIT_VERSION=$(echo "${GIT_VERSION}" | sed "s/-\([0-9]\{1,\}\)-g\([0-9a-f]\{14\}\)$/.\1\+\2/")
    elif [[ "${DASHES_IN_VERSION}" == "--" ]] ; then
      # We have distance to base tag (v1.1.0-1-gCommitHash)
      GIT_VERSION=$(echo "${GIT_VERSION}" | sed "s/-g\([0-9a-f]\{14\}\)$/+\1/")
    fi
    if [[ "${GIT_TREE_STATE}" == "dirty" ]]; then
      # git describe --dirty only considers changes to existing files, but
      # that is problematic since new untracked .go files affect the build,
      # so use our idea of "dirty" from git status instead.
      GIT_VERSION+="-dirty"
    fi


    # If GIT_VERSION is not a valid Semantic Version, then refuse to build.
    if ! [[ "${GIT_VERSION}" =~ ^v([0-9]+)\.([0-9]+)(\.[0-9]+)?(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]; then
      echo "GIT_VERSION should be a valid Semantic Version. Current value: ${GIT_VERSION}"
      echo "Please see more details here: https://semver.org"
      exit 1
    fi
  fi
fi

echo "-X 'github.com/aylei/kubectl-debug/version.gitVersion=${GIT_VERSION}'"
