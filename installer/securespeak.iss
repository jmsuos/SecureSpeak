; SecureSpeak Windows installer (Inno Setup 6.3+)
;
; Build (from the repository root, after building dist\windows\SecureSpeak.exe):
;   ISCC.exe /DAppVersion=3.0.0 /DAppVersionNum=3.0.0.0 installer\securespeak.iss
;
; Installs per user into %LOCALAPPDATA%\Programs\SecureSpeak by default (no
; administrator rights needed). Choosing "install for all users" in the first
; dialog installs into Program Files instead.

#ifndef AppVersion
  #define AppVersion "3.0.0"
#endif
#ifndef AppVersionNum
  #define AppVersionNum "3.0.0.0"
#endif
#ifndef SourceDir
  #define SourceDir "..\dist\windows"
#endif

[Setup]
; Never change AppId: Windows uses it to recognise upgrades and the uninstaller.
AppId={{6B0C5E2A-3D7F-4C1B-9E8A-5F2D1C7A9B44}
AppName=SecureSpeak
AppVersion={#AppVersion}
AppVerName=SecureSpeak {#AppVersion}
AppPublisher=SecureSpeak
AppPublisherURL=https://github.com/jmsuos/SecureSpeak
AppSupportURL=https://github.com/jmsuos/SecureSpeak/issues
AppUpdatesURL=https://github.com/jmsuos/SecureSpeak/releases
VersionInfoVersion={#AppVersionNum}
VersionInfoProductName=SecureSpeak
VersionInfoDescription=SecureSpeak Setup
DefaultDirName={autopf}\SecureSpeak
DefaultGroupName=SecureSpeak
DisableProgramGroupPage=yes
DisableDirPage=auto
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=commandline dialog
UsePreviousPrivileges=yes
OutputDir=..\dist\installer
OutputBaseFilename=SecureSpeak-Setup-{#AppVersion}
SetupIconFile=securespeak.ico
UninstallDisplayIcon={app}\SecureSpeak.exe
UninstallDisplayName=SecureSpeak
WizardStyle=modern
Compression=lzma2/max
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
CloseApplications=force
RestartApplications=no
ShowLanguageDialog=auto

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "dutch"; MessagesFile: "compiler:Languages\Dutch.isl"

[CustomMessages]
english.StartupTask=Start SecureSpeak when I sign in to Windows
dutch.StartupTask=SecureSpeak starten wanneer ik me aanmeld bij Windows
english.OtherTasks=Other:
dutch.OtherTasks=Overig:
english.RemoveData=Also delete your SecureSpeak settings and identity key?%n%nChoose No to keep them: you keep the same identity (and your roles on servers) if you install SecureSpeak again.
dutch.RemoveData=Ook je SecureSpeak-instellingen en identiteitssleutel verwijderen?%n%nKies Nee om ze te bewaren: dan houd je dezelfde identiteit (en je rollen op servers) als je SecureSpeak opnieuw installeert.

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
Name: "startup"; Description: "{cm:StartupTask}"; GroupDescription: "{cm:OtherTasks}"; Flags: unchecked

[Files]
Source: "{#SourceDir}\SecureSpeak.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\SecureSpeak"; Filename: "{app}\SecureSpeak.exe"; Comment: "End-to-end encrypted voice chat"
Name: "{autodesktop}\SecureSpeak"; Filename: "{app}\SecureSpeak.exe"; Tasks: desktopicon

[Registry]
; HKA = current user for per-user installs, all users for machine-wide installs
Root: HKA; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "SecureSpeak"; ValueData: """{app}\SecureSpeak.exe"""; Flags: uninsdeletevalue; Tasks: startup

[Run]
Filename: "{app}\SecureSpeak.exe"; Description: "{cm:LaunchProgram,SecureSpeak}"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{sys}\taskkill.exe"; Parameters: "/F /IM SecureSpeak.exe"; Flags: runhidden; RunOnceId: "StopSecureSpeak"

[Code]
// Stop a running SecureSpeak before files are replaced (it has no window of
// its own that the Restart Manager could close politely).
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM SecureSpeak.exe', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Result := '';
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if (CurUninstallStep = usPostUninstall) and (not UninstallSilent) then
  begin
    if MsgBox(CustomMessage('RemoveData'), mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
      DelTree(ExpandConstant('{userappdata}\SecureSpeak'), True, True, True);
  end;
end;
