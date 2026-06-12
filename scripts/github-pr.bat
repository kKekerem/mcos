@echo off
setlocal enabledelayedexpansion

:: github-pr.bat
:: Automates creating a branch, pushing changes, and opening a PR to kkekerem/mcos
:: Usage: scripts\github-pr.bat [branch-name]

set "BRANCH=%~1"
if "%BRANCH%"=="" set "BRANCH=feat/ui-fixes"

echo ===================================================
echo MCOS GitHub PR Automation
echo Branch: %BRANCH%
echo Repository: kkekerem/mcos
echo ===================================================

:: Check if git is installed
where git >nul 2>nul
if %errorlevel% neq 0 (
    echo [ERROR] Git is not installed or not in PATH.
    exit /b 1
)

:: Check if gh CLI is installed
where gh >nul 2>nul
if %errorlevel% neq 0 (
    echo [ERROR] GitHub CLI (gh) is not installed or not in PATH.
    echo Please install it from https://cli.github.com/
    exit /b 1
)

:: Create or switch to branch
echo [INFO] Checking out branch %BRANCH%...
git checkout -b "%BRANCH%" >nul 2>nul
if %errorlevel% neq 0 (
    git checkout "%BRANCH%"
)

:: Stage all files
echo [INFO] Staging files...
git add .

:: Check if there are changes to commit
git diff --cached --quiet
if %errorlevel% equ 0 (
    echo [INFO] No new changes to commit.
    goto push_pr
)

:: Commit changes
echo [INFO] Committing changes...
git commit -m "Enhance UI with Unicode/Emoji/Turkish support and fix USB detection"

:push_pr
:: Push the branch
echo [INFO] Pushing branch to origin...
git push -u origin "%BRANCH%"
if %errorlevel% neq 0 (
    echo [ERROR] Failed to push branch. Ensure your remote is configured correctly.
    exit /b 1
)

:: Create Pull Request
echo [INFO] Creating Pull Request on kkekerem/mcos...
gh pr create --repo kkekerem/mcos --title "Enhance UI with Unicode/Emoji/Turkish support and fix USB detection" --body "This PR introduces several UI enhancements: proper Unicode and Box Drawing support for both Go and Rust TUIs (removing the ASCII fallback), text input highlighting for OOBE fields, and a fix for sysfs USB detection to accurately identify removable drives. Windows batch script for automated PR creation is also included."
if %errorlevel% eq 0 (
    echo [SUCCESS] Pull Request created successfully!
) else (
    echo [ERROR] Failed to create Pull Request. You may need to run 'gh auth login'.
)

endlocal
exit /b 0
