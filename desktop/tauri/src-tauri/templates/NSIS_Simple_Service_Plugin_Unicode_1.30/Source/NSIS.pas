{
    Original Code from
    (C) 2001 - Peter Windridge

    Code in separate unit and some changes
    2003 by Bernhard Mayer

    Fixed and formatted by Brett Dever
    http://editor.nfscheats.com/

    simply include this unit in your plugin project and export
    functions as needed
}

unit nsis;

interface

uses
  Winapi.Windows, Winapi.CommCtrl, System.SysUtils;

type
  VarConstants = (
    INST_0,       // $0
    INST_1,       // $1
    INST_2,       // $2
    INST_3,       // $3
    INST_4,       // $4
    INST_5,       // $5
    INST_6,       // $6
    INST_7,       // $7
    INST_8,       // $8
    INST_9,       // $9
    INST_R0,      // $R0
    INST_R1,      // $R1
    INST_R2,      // $R2
    INST_R3,      // $R3
    INST_R4,      // $R4
    INST_R5,      // $R5
    INST_R6,      // $R6
    INST_R7,      // $R7
    INST_R8,      // $R8
    INST_R9,      // $R9
    INST_CMDLINE, // $CMDLINE
    INST_INSTDIR, // $INSTDIR
    INST_OUTDIR,  // $OUTDIR
    INST_EXEDIR,  // $EXEDIR
    INST_LANG,    // $LANGUAGE
    __INST_LAST
    );
  TVariableList = INST_0..__INST_LAST;

type
  PluginCallbackMessages = (
    NSPIM_UNLOAD,   // This is the last message a plugin gets, do final cleanup
    NSPIM_GUIUNLOAD // Called after .onGUIEnd
    );
  TNSPIM = NSPIM_UNLOAD..NSPIM_GUIUNLOAD;

  //TPluginCallback = function (const NSPIM: Integer): Pointer; cdecl;

  TExecuteCodeSegment = function (const funct_id: Integer; const parent: HWND): Integer;  stdcall;
  Tvalidate_filename = procedure (const filename: PChar); stdcall;
  TRegisterPluginCallback = function (const DllInstance: HMODULE; const CallbackFunction: Pointer): Integer; stdcall;

  pexec_flags_t = ^exec_flags_t;
  exec_flags_t = record
    autoclose: Integer;
    all_user_var: Integer;
    exec_error: Integer;
    abort: Integer;
    exec_reboot: Integer;
    reboot_called: Integer;
    XXX_cur_insttype: Integer;
    plugin_api_version: Integer;
    silent: Integer;
    instdir_error: Integer;
    rtl: Integer;
    errlvl: Integer;
    alter_reg_view: Integer;
    status_update: Integer;
  end;

  pextrap_t = ^extrap_t;
  extrap_t = record
    exec_flags: Pointer; // exec_flags_t;
    exec_code_segment: TExecuteCodeSegment; //  TFarProc;
    validate_filename: Pointer; // Tvalidate_filename;
    RegisterPluginCallback: Pointer; //TRegisterPluginCallback;
  end;

  pstack_t = ^stack_t;
  stack_t = record
    next: pstack_t;
    text: PChar;
  end;

var
  g_stringsize: integer;
  g_stacktop: ^pstack_t;
  g_variables: PChar;
  g_hwndParent: HWND;
  g_hwndList: HWND;
  g_hwndLogList: HWND;
  g_extraparameters: pextrap_t;

procedure Init(const hwndParent: HWND; const string_size: integer; const variables: PChar; const stacktop: pointer; const extraparameters: pointer = nil);

function LogMessage(Msg : String): BOOL;
function Call(NSIS_func : String) : Integer;
function PopString(): string;
procedure PushString(const str: string='');
function GetUserVariable(const varnum: TVariableList): string;
procedure SetUserVariable(const varnum: TVariableList; const value: string);
procedure NSISDialog(const text, caption: string; const buttons: integer);

implementation

procedure Init(const hwndParent: HWND; const string_size: integer; const variables: PChar; const stacktop: pointer; const extraparameters: pointer = nil);
begin
  g_stringsize := string_size;
  g_hwndParent := hwndParent;
  g_stacktop   := stacktop;
  g_variables  := variables;
  g_hwndList   := FindWindowEx(FindWindowEx(g_hwndParent, 0, '#32770', nil), 0,'SysListView32', nil);
  g_extraparameters := extraparameters;
end;


function Call(NSIS_func : String) : Integer;
var
  codeoffset: Integer; //The ID of nsis function
begin
  Result := 0;
  codeoffset := StrToIntDef(NSIS_func, 0);
  if (codeoffset <> 0) and (g_extraparameters <> nil) then
    begin
    codeoffset := codeoffset - 1;
    Result := g_extraparameters.exec_code_segment(codeoffset, g_hwndParent);
    end;
end;

function LogMessage(Msg : String): BOOL;
var
  ItemCount : Integer;
  item: TLVItem;
begin
  Result := FAlse;
  if g_hwndList = 0 then exit;
  FillChar( item, sizeof(item), 0 );
  ItemCount := SendMessage(g_hwndList, LVM_GETITEMCOUNT, 0, 0);
  item.iItem := ItemCount;
  item.mask := LVIF_TEXT;
  item.pszText := PChar(Msg);
  ListView_InsertItem(g_hwndList, item);
  ListView_EnsureVisible(g_hwndList, ItemCount, TRUE);
end;

function PopString(): string;
var
  th: pstack_t;
begin
  if integer(g_stacktop^) <> 0 then begin
    th := g_stacktop^;
    Result := PChar(@th.text);
    g_stacktop^ := th.next;
    GlobalFree(HGLOBAL(th));
  end;
end;

procedure PushString(const str: string='');
var
  th: pstack_t;
begin
  if integer(g_stacktop) <> 0 then begin
    th := pstack_t(GlobalAlloc(GPTR, SizeOf(stack_t) + g_stringsize));
    lstrcpyn(@th.text, PChar(str), g_stringsize);
    th.next := g_stacktop^;
    g_stacktop^ := th;
  end;
end;

function GetUserVariable(const varnum: TVariableList): string;
begin
  if (integer(varnum) >= 0) and (integer(varnum) < integer(__INST_LAST)) then
    Result := g_variables + integer(varnum) * g_stringsize
  else
    Result := '';
end;

procedure SetUserVariable(const varnum: TVariableList; const value: string);
begin
  if (value <> '') and (integer(varnum) >= 0) and (integer(varnum) < integer(__INST_LAST)) then
    lstrcpy(g_variables + integer(varnum) * g_stringsize, PChar(value))
end;

procedure NSISDialog(const text, caption: string; const buttons: integer);
var
  hwndOwner: HWND;
begin
  hwndOwner := g_hwndParent;
  if not IsWindow(g_hwndParent) then hwndOwner := 0; // g_hwndParent is not valid in NSPIM_[GUI]UNLOAD
  MessageBox(hwndOwner, PChar(text), PChar(caption), buttons);
end;

begin

end.

