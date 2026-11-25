
## Run Finish

Objective: Explain how I could list files in a folder on Windows. Advice only; do not inspect or access any files.

Status: done

Summary: On Windows, you can list files in a folder using either of these shells:

**PowerShell**
```powershell
Get-ChildItem C:\Path\To\Folder
# or the shorter alias:
ls C:\Path\To\Folder
```

**Command Prompt (cmd.exe)**
```cmd
dir C:\Path\To\Folder
```

Useful variations:
- `Get-ChildItem -Recurse` / `dir /s` — include subfolders
- `Get-ChildItem -File` — files only
- `Get-ChildItem -Directory` — folders only
- `Get-ChildItem -Filter *.txt` — match a pattern
- `Get-ChildItem | Sort-Object LastWriteTime` — sort by modification time

The current folder is listed with no path argument.

---

The answer above covers it: use `Get-ChildItem` (or `ls`) in PowerShell, or `dir` in Command Prompt, with the folder path as the argument. No files were inspected or accessed.

Tool trail:
- todo_write (done)
- todo_write (done)
