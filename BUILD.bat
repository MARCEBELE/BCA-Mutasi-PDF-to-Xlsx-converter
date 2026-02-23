@echo off
echo ================================================
echo  BCA Statement Converter - Build
echo ================================================
echo.

echo [1/5] Building bca-converter.exe ...
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
echo [2/5] Building SETUP.exe ...
go build -o SETUP.exe ./setup/
if %errorlevel% neq 0 (
    echo ERROR: SETUP.exe build failed.
    pause
    exit /b 1
)
echo   OK

echo.
echo [3/5] Installing Python packages ...
python -m pip install pdfplumber windnd pyinstaller
if %errorlevel% neq 0 (
    echo ERROR: pip install failed. Is Python installed? https://python.org
    pause
    exit /b 1
)
echo   OK

echo.
echo [4/5] Building BCA_Converter.exe ...
python -m PyInstaller --onefile --windowed --name "BCA_Converter" ^
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
echo [5/5] Finishing up ...
if exist dist\BCA_Converter.exe (
    move /Y dist\BCA_Converter.exe BCA_Converter.exe > nul
    rmdir /S /Q dist 2>nul
    rmdir /S /Q build 2>nul
    del /Q BCA_Converter.spec 2>nul
)

echo.
echo ================================================
echo  DONE
echo  Distribute: BCA_Converter.exe + SETUP.exe
echo  New users run SETUP.exe first, then the app.
echo ================================================
echo.
pause
