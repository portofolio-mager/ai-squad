#!/bin/bash

# Exit early if CLAUDE_HOOKS_DISABLE is set to true
if [ "$CLAUDE_HOOKS_DISABLE" = "true" ]; then
    echo "[Git Commit Hook] Disabled by CLAUDE_HOOKS_DISABLE environment variable"
    exit 0
fi

# Find claude command
CLAUDE_CMD=""

# First, check if claude is in PATH (handles npm global installs and standard installs)
if command -v claude &> /dev/null; then
    CLAUDE_CMD="claude"
else
    # Try common locations
    for cmd in "$HOME/.claude/local/claude" "/usr/local/bin/claude" "/opt/homebrew/bin/claude" "$(npm bin -g 2>/dev/null)/claude"; do
        if [ -x "$cmd" ]; then
            CLAUDE_CMD="$cmd"
            break
        fi
    done
fi

if [ -z "$CLAUDE_CMD" ]; then
    echo "❌ Error: claude command not found"
    echo "Please ensure Claude CLI is installed and accessible"
    echo "Install via npm: npm install -g @anthropic/claude-cli"
    exit 1
fi

# Check if there are any changes to commit
if [ -z "$(git status --porcelain)" ]; then
    echo "No changes to commit"
    exit 0
fi

echo "🤖 Requesting Claude to generate commit message..."

# Get list of changed files
CHANGED_FILES=$(git diff --name-only)
STAGED_FILES=$(git diff --cached --name-only)
UNTRACKED_FILES=$(git ls-files --others --exclude-standard)

# Add all changes
if [ -n "$CHANGED_FILES" ] || [ -n "$UNTRACKED_FILES" ]; then
    git add -A
fi

# Prepare the prompt for Claude
PROMPT="Generate a concise git commit message for the following changes. Only output the commit message itself, no explanations or extra text.

Git Status:
$(git status --porcelain)

Changed Files:
$CHANGED_FILES
$UNTRACKED_FILES

Git Diff:
$(git diff --cached)"

# Call Claude to generate the commit message
COMMIT_MESSAGE=$(echo "$PROMPT" | CLAUDE_HOOKS_DISABLE=true $CLAUDE_CMD --output-format text)

echo "📝 Generated commit message: $COMMIT_MESSAGE"

# Check if Claude returned a message
if [ -z "$COMMIT_MESSAGE" ]; then
    echo "❌ Failed to generate commit message"
    exit 1
fi

# Make the commit with the generated message
git commit -m "$COMMIT_MESSAGE"

if [ $? -eq 0 ]; then
    echo "✅ Commit successful!"
    exit 0
else
    echo "❌ Commit failed"
    exit 1
fi