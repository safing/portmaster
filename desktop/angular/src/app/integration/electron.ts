import { BrowserIntegrationService } from "./browser";
import { AppInfo, PortmasterDir, ProcessInfo } from "./integration";

export class ElectronIntegrationService extends BrowserIntegrationService {

  openExternal(pathOrUrl: string): Promise<void> {
    if (!!window.app) {
      return window.app.openExternal(pathOrUrl);
    }

    return Promise.reject('No electron API available')
  }

  openDir(kind: PortmasterDir): Promise<void> {
    if (!window.app) {
      return Promise.reject('No electron API available')
    }
    if (kind !== 'data') {
      return Promise.reject('Not supported in electron')
    }

    return window.app.getInstallDir()
      .then(dir => window.app.openExternal(dir));
  }

  getAppIcon(info: ProcessInfo): Promise<string> {
    if (!!window.app) {
      return window.app.getFileIcon(info.execPath)
    }

    return Promise.reject('No electron API available')
  }

  getAppInfo(_: ProcessInfo): Promise<AppInfo> {
    return Promise.reject('Not supported in electron')
  }

  exitApp(): Promise<void> {
    if (!!window.app) {
      window.app.exitApp();
    }

    return Promise.resolve();
  }

  onExitRequest(cb: () => void): () => void {
    let listener = (event: MessageEvent<any>) => {
      if (event.data === 'on-app-close') {
        cb();
      }
    }

    window.addEventListener('message', listener);

    return () => {
      window.removeEventListener('message', listener)
    }
  }
}
