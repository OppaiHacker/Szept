; Windows installer for szept-wails. Built by `make windows-installer`:
;   makensis -DVERSION=v0.2.0 -DEXE=bin/szept-wails.exe -DICON=bin/szept.ico \
;            -DWEBVIEW=bin/MicrosoftEdgeWebview2Setup.exe -DOUT=bin/szept-setup.exe
;
; Per-user install into %LOCALAPPDATA%\Programs\szept, no admin rights: the
; app replaces its own exe when it updates (internal/update), which a
; Program Files install would not allow.

Unicode true
!include "MUI2.nsh"

!ifndef VERSION
  !define VERSION "dev"
!endif
!define UNINST "Software\Microsoft\Windows\CurrentVersion\Uninstall\szept"
; WebView2 Runtime's client ID, the same in every install
!define WV2 "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

Name "szept"
OutFile "${OUT}"
InstallDir "$LOCALAPPDATA\Programs\szept"
InstallDirRegKey HKCU "${UNINST}" "InstallLocation"
RequestExecutionLevel user
SetCompressor /SOLID lzma

!define MUI_ICON "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_FINISHPAGE_RUN "$INSTDIR\szept.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Uruchom szepta"

!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "Polish"
!insertmacro MUI_LANGUAGE "English"

Section "szept" SecMain
  SectionIn RO
  SetOutPath "$INSTDIR"
  ; a running szept holds the exe open; NSIS then offers retry
  File "/oname=szept.exe" "${EXE}"
  Delete "$INSTDIR\szept.exe.old"
  WriteUninstaller "$INSTDIR\uninstall.exe"
  CreateShortcut "$SMPROGRAMS\szept.lnk" "$INSTDIR\szept.exe"

  WriteRegStr HKCU "${UNINST}" "DisplayName" "szept"
  WriteRegStr HKCU "${UNINST}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST}" "Publisher" "szept"
  WriteRegStr HKCU "${UNINST}" "DisplayIcon" "$INSTDIR\szept.exe"
  WriteRegStr HKCU "${UNINST}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegDWORD HKCU "${UNINST}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST}" "NoRepair" 1

  ; Windows 11 ships WebView2; a bare Windows 10 needs the runtime. NSIS is
  ; 32-bit, so this HKLM read already lands in WOW6432Node, where the
  ; machine-wide runtime registers. Missing or "0.0.0.0" = not installed.
  ReadRegStr $0 HKLM "${WV2}" "pv"
  StrCmp $0 "" +2
  StrCmp $0 "0.0.0.0" 0 wv2ok
  ReadRegStr $0 HKCU "${WV2}" "pv"
  StrCmp $0 "" +2
  StrCmp $0 "0.0.0.0" 0 wv2ok
  DetailPrint "Instaluję Microsoft Edge WebView2 Runtime..."
  SetOutPath "$PLUGINSDIR"
  File "/oname=wv2setup.exe" "${WEBVIEW}"
  ExecWait '"$PLUGINSDIR\wv2setup.exe" /silent /install'
  SetOutPath "$INSTDIR"
  wv2ok:
SectionEnd

Section "Skrót na pulpicie" SecDesktop
  CreateShortcut "$DESKTOP\szept.lnk" "$INSTDIR\szept.exe"
SectionEnd

Function .onInit
  InitPluginsDir
FunctionEnd

; Messages, keys and settings stay: they are the user's, not the program's.
Section "Uninstall"
  Delete "$INSTDIR\szept.exe"
  Delete "$INSTDIR\szept.exe.old"
  Delete "$INSTDIR\szept.exe.new"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\szept.lnk"
  Delete "$DESKTOP\szept.lnk"
  DeleteRegKey HKCU "${UNINST}"
SectionEnd
