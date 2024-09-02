import { AppInfo, IntegrationService, ProcessInfo } from "./integration";
import { writeText } from '@tauri-apps/plugin-clipboard-manager';
import { open } from '@tauri-apps/plugin-shell';
import { listen, once } from '@tauri-apps/api/event';
import { invoke } from '@tauri-apps/api/core'
import { getCurrentWindow, Window } from '@tauri-apps/api/window';

// Returns a new uuidv4. If crypto.randomUUID is not available it fals back to
// using Math.random(). While this is not as random as it should be it's still
// enough for our use-case here (which is just to generate a random response-id).
function uuid(): string {
  if (typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }

  // This one is not really random and not RFC compliant but serves enough for fallback
  // purposes if the UI is opened in a browser that does not yet support randomUUID
  console.warn('Using browser with lacking support for crypto.randomUUID()');

  return Date.now().toString(36) + Math.random().toString(36).substring(2);
}

function asyncInvoke<T>(method: string, args: object): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const eventId = uuid();

    once<T & { error: string }>(eventId, (event) => {
      if (typeof event.payload === 'object' && 'error' in event.payload) {
        reject(event.payload);
        return
      }

      resolve(event.payload);
    })

    invoke<string>(method, {
      ...args,
      responseId: eventId,
    }).catch((err: any) => {
      console.error("tauri:invoke rejected: ", method, args, err);
      reject(err)
    });
  })
}

export type ServiceManagerStatus = 'Running' | 'Stopped' | 'NotFound' | 'unsupported service manager' | 'unsupported operating system';

export class TauriIntegrationService implements IntegrationService {
  private withPrompts = false;

  constructor() {
    this.shouldHandlePrompts()
      .then(result => {
        this.withPrompts = result;
      });

    // listen for the portmaster:show event that is emitted
    // when tauri want's to tell us that we should make our
    // window visible.
    listen("portmaster:show", () => {
      this.openApp();
    })
  }

  writeToClipboard(text: string): Promise<void> {
    return writeText(text);
  }

  openExternal(pathOrUrl: string): Promise<void> {
    return open(pathOrUrl);
  }

  getInstallDir(): Promise<string> {
    return Promise.reject("not yet supported in tauri")
  }

  getAppInfo(info: ProcessInfo): Promise<AppInfo> {
    return asyncInvoke("get_app_info", {
      ...info,
    })
  }

  getAppIcon(info: ProcessInfo): Promise<string> {
    return this.getAppInfo(info)
      .then(info => info.icon_dataurl)
  }

  exitApp(): Promise<void> {
    // we have two options here:
    //  - close(): close the native tauri window and release all resources of it.
    //             this has the disadvantage that if the user re-opens the window,
    //             it will take slightly longer because angular need to re-bootstrap
    //             the application.
    //
    //             IMPORTANT: the angular application will automatically launch prompt
    //             windows via the tauri window interface. If we would call close(),
    //             those prompts wouldn't work anymore because the angular app would not
    //             be running in the background.
    //
    //  - hide(): just set the window visibility to false. The advantage is that angular
    //            is still running and interacting with portmaster but it also means that
    //            we waste some system resources due to tauri window objects and the angular
    //            application.

    getCurrentWindow().hide()

    return Promise.resolve();
  }

  // Tauri specific functions that are not defined in the IntegrationService interface.
  // to use those methods you must check if integration instanceof TauriIntegrationService.

  async shouldShow(): Promise<boolean> {
    try {
      const response = await invoke<string>("should_show");
      return response === "show";
    } catch (err) {
      console.error(err);
      return true;
    }
  }

  async shouldHandlePrompts(): Promise<boolean> {
    try {
      const response = await invoke<string>("should_handle_prompts")
      return response === "true"
    } catch (err) {
      console.error(err);
      return false;
    }
  }

  get_state(_: string): Promise<string> {
    return invoke<string>("get_state");
  }

  set_state(key: string, value: string): Promise<void> {
    return invoke<void>("set_state", {
      key,
      value
    })
  }

  getServiceManagerStatus(): Promise<ServiceManagerStatus> {
    return asyncInvoke("get_service_manager_status", {})
  }

  startService(): Promise<any> {
    return asyncInvoke("start_service", {});
  }

  onExitRequest(cb: () => void): () => void {
    let unlisten: () => void = () => undefined;

    listen('exit-requested', () => {
      cb();
    }).then(cleanup => {
      unlisten = cleanup;
    })

    return () => {
      unlisten();
    }
  }

  openApp() {
    Window.getByLabel("splash").then(splash => { splash?.close();});
    const current = Window.getCurrent()

    current.isVisible()
      .then(visible => {
        if (!visible) {
          current.show();
          current.setFocus();
        }
      });
  }

  closePrompt() {
    Window.getByLabel("prompt").then(window => { window?.close();});
  }

  openPrompt() {
    if (!this.withPrompts) {
      return;
    }

    Window.getByLabel("prompt").then(prompt => { 
      if (prompt) {
        return;
      }

      const promptWindow = new Window("prompt", {
        alwaysOnTop: true,
        decorations: false,
        minimizable: false,
        maximizable: false,
        resizable: false,
        title: 'Portmaster Prompt',
        visible: false, // the prompt marks it self as visible.
        skipTaskbar: true,
        closable: false,
        center: true,
        width: 600,
        height: 300,

        // in src/main.ts we check the current location path
        // and if it matches /prompt, we bootstrap the PromptEntryPointComponent
        // instead of the AppComponent.
        url: `http://${window.location.host}/prompt`,
      } as any)

      promptWindow.once("tauri://error", (err) => {
        console.error(err);
      });
    });
  }
}
