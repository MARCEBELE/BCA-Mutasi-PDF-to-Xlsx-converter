@echo off
echo ================================================
echo  BCA Statement Converter - Build
echo ================================================
echo.

echo [1/4] Building bca-converter.exe ...
set GOOS=windows
set GOARCH=amd64
go build -o bca-converter.exe .
if %errorlevel% neq 0 (
    echo ERROR: Go build failed. Is Go installed? https://go.dev/dl/
    pause
    exit /b 1
)
echo   OK

echo.
echo [2/4] Installing Python packages ...
pip install pdfplumber windnd pyinstaller --quiet
if %errorlevel% neq 0 (
    echo ERROR: pip install failed. Is Python installed? https://python.org
    pause
    exit /b 1
)
echo   OK

echo.
echo [3/4] Building BCA_Converter.exe ...
pyinstaller --onefile --windowed --name "BCA_Converter" ^
    --add-data "bca-converter.exe;." ^
    --add-data "pdf_to_txt.py;." ^
    --hidden-import windnd ^
    bca_ui.py
if %errorlevel% neq 0 (
    echo ERROR: PyInstaller failed - see errors above.
    pause
    exit /b 1
)

echo.
echo [4/4] Finishing up ...
if exist dist\BCA_Converter.exe (
    move /Y dist\BCA_Converter.exe BCA_Converter.exe > nul
    rmdir /S /Q dist 2>nul
    rmdir /S /Q build 2>nul
    del /Q BCA_Converter.spec 2>nul
)

echo.
echo ================================================
echo  DONE - double-click BCA_Converter.exe to start
echo ================================================
echo.
pause