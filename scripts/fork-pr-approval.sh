#!/bin/bash

# Starts CI for a fork PR after a maintainer comments /ok-to-test <SHA>.
# .github/workflows/fork-pr-approval.yaml runs it from a checkout of master
# with full history. It never checks out or runs the PR's code.
#
# Environment:
#   PR         the PR number
#   COMMENTER  who wrote the comment
#   COMMENT    the comment text
#   GH_TOKEN   token for the gh CLI
#
# If CI can't be started, the reason goes to GITHUB_OUTPUT as "reason", and
# the workflow comments it on the PR.

set -e

# Saves the reason for the PR comment, then stops.
fail() {
  echo "reason=$1" >> "$GITHUB_OUTPUT"
  echo "::error::$1"
  exit 1
}

# Only users with write access can approve.
permission=$(gh api "repos/$GITHUB_REPOSITORY/collaborators/$COMMENTER/permission" --jq .permission)
if [ "$permission" != admin ] && [ "$permission" != write ]; then
  fail "@$COMMENTER has $permission access, and /ok-to-test needs write access."
fi

# The comment must be "/ok-to-test <full 40-character SHA>".
# Trailing spaces and newlines are allowed.
pattern='^/ok-to-test +([0-9a-f]{40})[[:space:]]*$'
if [[ ! "$COMMENT" =~ $pattern ]]; then
  fail "Comment /ok-to-test followed by the full 40-character SHA of the commit you reviewed."
fi
approved_sha=${BASH_REMATCH[1]}

git fetch -q origin "refs/pull/$PR/head:pr-head"
head_sha=$(git rev-parse pr-head)

# Code pushed after the review must not run.
if [ "$head_sha" != "$approved_sha" ]; then
  fail "The PR's head is $head_sha, not $approved_sha. Review the new commits, then comment again with the new SHA."
fi

# ci.yaml runs from the branch pushed below, so the PR must not change it.
# The three dots compare with the commit the PR started from, so a PR
# that is behind master isn't blamed for changes made on master.
changed=$(git diff --name-only origin/master...pr-head -- .github/)
if [ -n "$changed" ]; then
  echo "$changed"
  fail "This PR changes files under .github/, so CI can't run it from a fork."
fi

# Each commit gets its own branch, and ci.yaml deletes it when its run ends.
branch="fork-pr/$PR-${head_sha::7}"
if git ls-remote --exit-code origin "refs/heads/$branch" > /dev/null; then
  fail "$branch already exists, so CI may already be running for this commit. If it isn't, delete the branch and comment again."
fi
git push -q origin "$head_sha:refs/heads/$branch"

# If CI doesn't start, nothing else deletes the branch.
if ! gh workflow run ci.yaml --repo "$GITHUB_REPOSITORY" --ref "$branch" -f pr-number="$PR"; then
  gh api --method DELETE "repos/$GITHUB_REPOSITORY/git/refs/heads/$branch" > /dev/null \
    || echo "::warning::Couldn't delete $branch. Delete it by hand."
  fail "CI couldn't be started on $branch."
fi
echo "Started CI for $head_sha on $branch"
