# AI Squad Configuration Example

This repository demonstrates the per-repository configuration system for ai-squad.

## Usage

- Press `w` to open the current instance in your configured IDE (Visual Studio Code in this case)
- Press `i` in diff view to open the current file in your IDE 
- Press `x` in diff view to open the current file in your external diff tool


### Per-Repository Configuration
Option 1: Add to your `CLAUDE.md` file:
```markdown
[ai-squad]
ide_command: code
diff_command: code --diff
```

Option 2: Create `.ai-squad/config.json` in your repository:
```json
{
  "ide_command": "code",
  "diff_command": "meld"
}
```

Per-repository configuration takes precedence over global configuration.