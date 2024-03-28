
export interface AppInfo {
  app_name: string;
  comment: string;
  icon_dataurl: string;
  icon_path: string;
}

export interface ProcessInfo {
  execPath: string;
  cmdline: string;
  pid: number;
  matchingPath: string;
}

export interface IntegrationService {
  /** writeToClipboard copies text to the system clipboard */
  writeToClipboard(text: string): Promise<void>;

  /** openExternal opens a file or URL in an external window */
  openExternal(pathOrUrl: string): Promise<void>;

  /** Gets the path to the portmaster installation directory */
  getInstallDir(): Promise<string>;

  /** Load application information (currently linux only) */
  getAppInfo(info: ProcessInfo): Promise<AppInfo>;

  /** Loads the application icon as a dataurl */
  getAppIcon(info: ProcessInfo): Promise<string>;

  /** Closes the application, does not return */
  exitApp(): Promise<void>;

  /** Registers a listener for on-close requests. */
  onExitRequest(cb: () => void): () => void;
}




