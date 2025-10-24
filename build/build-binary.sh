#! /bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"

repo_path=${1}
build_cmd=${2}

if [[ -z ${repo_path} ]] || [[ -z ${build_cmd} ]]; then
  echo "error: must provide the repo path and build command as positional arguments."
  exit 1
fi

if ! [[ -d ${SCRIPT_DIR}/../${repo_path} ]]; then
  echo "error: failed to find repo path '${repo_path}'."
  exit 1
fi

repo_path=${SCRIPT_DIR}/../${repo_path}
git_repo=$(basename "${repo_path}")

cd "${repo_path}" || exit 1

# Set branch using .gitmodules config
parent_gitmodules="${SCRIPT_DIR}/../.gitmodules"
if [[ -f "${parent_gitmodules}" ]]; then
  submodule_branch=$(git config --file "${parent_gitmodules}" --get "submodule.${git_repo}.branch" 2>/dev/null || echo "")
  if [[ -n "${submodule_branch}" ]]; then
    echo "* Creating branch '${submodule_branch}' for version generation"
    case "${submodule_branch}" in
      release-*)
        git checkout -b "${submodule_branch}" 2>/dev/null || git checkout "${submodule_branch}"
        ;;
      v*)
        git tag "${submodule_branch}" || git checkout "${submodule_branch}"
        ;;
      default)
        echo "error: unrecognized branch '${submodule_branch}'"
        exit 1
        ;;
    esac
  fi
fi

rm -rf vendor
go mod vendor
eval "${build_cmd}" || {
  echo "error: build command '${build_cmd}' failed for '${git_repo}'."
  exit 1
}
