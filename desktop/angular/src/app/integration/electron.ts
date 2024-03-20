import { BrowserIntegrationService } from "./browser";
import { AppInfo, ProcessInfo } from "./integration";

export class ElectronIntegrationService extends BrowserIntegrationService {

  openExternal(pathOrUrl: string): Promise<void> {
    if (!!window.app) {
      return window.app.openExternal(pathOrUrl);
    }

    return Promise.reject('No electron API available')
  }

  getInstallDir(): Promise<string> {
    if (!!window.app) {
      return window.app.getInstallDir()
    }

    return Promise.reject('No electron API available')
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
