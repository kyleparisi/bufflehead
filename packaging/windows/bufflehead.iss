; Inno Setup script — packages the Bufflehead Windows build into a single
; Setup.exe that installs to Program Files with Start Menu / desktop shortcuts
; and an uninstaller.
;
; A graphics.gd app is inherently multi-file: Bufflehead.exe loads
; windows_amd64.dll (the Go/DuckDB logic) from its own folder, which in turn
; needs the MinGW runtime DLLs (libstdc++-6, libgcc_s_seh-1, libwinpthread-1).
; The installer keeps them together under Program Files so end users never move
; the exe away from its dependencies (which breaks it).
;
; Built in CI:
;   iscc /DAppVersion=<x.y.z> /Oinstaller packaging\windows\bufflehead.iss

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
; Stable AppId so version upgrades replace the prior install in place.
AppId={{E137D830-64C0-4A52-B9A8-71192B2ACBC2}
AppName=Bufflehead
AppVersion={#AppVersion}
AppVerName=Bufflehead {#AppVersion}
AppPublisher=Bufflehead.app
DefaultDirName={autopf}\Bufflehead
DefaultGroupName=Bufflehead
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\Bufflehead.exe
OutputBaseFilename=Bufflehead-Setup
; Icon for Setup.exe itself — optional (passed by bin/build-windows as
; /DIconFile only when graphics/icon.ico was generated, i.e. ImageMagick is
; present). Shortcut icons come from the exe regardless.
#ifdef IconFile
SetupIconFile={#IconFile}
#endif
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Additional icons:"

[Files]
; The whole bundle produced by the workflow: Bufflehead.exe + windows_amd64.dll
; + the three MinGW runtime DLLs.
Source: "{#SourcePath}\..\..\releases\windows\amd64\*"; DestDir: "{app}"; Flags: recursesubdirs ignoreversion

[Icons]
Name: "{group}\Bufflehead"; Filename: "{app}\Bufflehead.exe"
Name: "{group}\Uninstall Bufflehead"; Filename: "{uninstallexe}"
Name: "{autodesktop}\Bufflehead"; Filename: "{app}\Bufflehead.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\Bufflehead.exe"; Description: "Launch Bufflehead"; Flags: nowait postinstall skipifsilent
