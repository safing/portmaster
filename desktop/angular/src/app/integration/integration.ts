
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

/** The Portmaster directories that can be opened via openDir(). */
export type PortmasterDir = 'bin' | 'data' | 'logs';

export interface IntegrationService {
  /** writeToClipboard copies text to the system clipboard */
  writeToClipboard(text: string): Promise<void>;

  /** openExternal opens a file or URL in an external window */
  openExternal(pathOrUrl: string): Promise<void>;

  /** Opens one of the Portmaster directories in the system file manager */
  openDir(kind: PortmasterDir): Promise<void>;

  /** Load application information (currently linux only) */
  getAppInfo(info: ProcessInfo): Promise<AppInfo>;

  /** Loads the application icon as a dataurl */
  getAppIcon(info: ProcessInfo): Promise<string>;

  /** Closes the application, does not return */
  exitApp(): Promise<void>;

  /** Registers a listener for on-close requests. */
  onExitRequest(cb: () => void): () => void;
}




