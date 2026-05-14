#! /bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"

usage() {
  echo "Usage: build-binary.sh <repo-path> <build-cmd> [--replace <old>=<new>[@version]] ..."
  echo ""
  echo "  repo-path   Path to the submodule relative to the repo root"
  echo "  build-cmd   Shell command to build the binary"
  echo "  --replace   Optional, repeatable. Passed directly to 'go mod edit -replace'."
  echo "              Example: --replace github.com/foo/bar=github.com/foo/bar@v1.2.3"
}

repo_path=${1:-""}
build_cmd=${2:-""}

if [[ -z ${repo_path} ]] || [[ -z ${build_cmd} ]]; then
  echo "error: must provide the repo path and build command as positional arguments."
  usage
  exit 1
fi

shift 2

go_replace_directives=()
while [[ $# -gt 0 ]]; do
  case "${1}" in
    --replace)
      if [[ -z "${2:-}" ]]; then
        echo "error: --replace requires an argument."
        usage
        exit 1
      fi
      go_replace_directives+=("${2}")
      shift 2
      ;;
    *)
      echo "error: unknown argument '${1}'."
      usage
      exit 1
      ;;
  esac
done

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

for _r in "${go_replace_directives[@]}"; do
  echo "* Applying go mod replace: ${_r}"
  go mod edit -replace "${_r}"
done

rm -rf vendor
go mod vendor
eval "${build_cmd}" || {
  echo "error: build command '${build_cmd}' failed for '${git_repo}'."
  exit 1
}
