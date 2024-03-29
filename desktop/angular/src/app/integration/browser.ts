import { AppInfo, IntegrationService, ProcessInfo } from "./integration";

export class BrowserIntegrationService implements IntegrationService {
  writeToClipboard(text: string): Promise<void> {
    if (!!navigator.clipboard) {
      return navigator.clipboard.writeText(text);
    }

    return Promise.reject(new Error(`Clipboard API not supported`))
  }

  openExternal(pathOrUrl: string): Promise<void> {
    window.open(pathOrUrl, '_blank')

    return Promise.resolve();
  }

  getInstallDir(): Promise<string> {
    return Promise.reject('Not supported in browser')
  }

  getAppIcon(_: ProcessInfo): Promise<string> {
    return Promise.reject('Not supported in browser')
  }

  getAppInfo(_: ProcessInfo): Promise<AppInfo> {
    return Promise.reject('Not supported in browser')
  }

  exitApp(): Promise<void> {
    window.close();

    return Promise.resolve();
  }

  onExitRequest(cb: () => void): () => void {
    // nothing to do, there
    return () => { }
  }
}

