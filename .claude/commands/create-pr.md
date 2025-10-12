---
description: Create a GitHub PR with auto-generated title and body based on changes
---

Create a pull request from the current branch to dev with an auto-generated title and body based on the git changes.

Run this command:
```bash
bash -c 'git status && git diff dev...HEAD --stat && git log dev..HEAD --oneline && echo "\n---\nCreating PR from $(git branch --show-current) to dev...\n" && git log dev..HEAD --pretty=format:"%s" | head -1 | { read title; summary=$(git log dev..HEAD --pretty=format:"- %s" | head -5); changes=$(git diff --name-only dev...HEAD | sed "s/^/- /g"); body=$(printf "## Summary\n%s\n\n## Changes\n%s\n\n🤖 Generated with [Claude Code](https://claude.ai/code)" "$summary" "$changes"); gh pr create --base dev --title "$title" --body "$body"; }'
```