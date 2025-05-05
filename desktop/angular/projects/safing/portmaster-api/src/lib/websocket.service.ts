import { Injectable, NgZone } from '@angular/core';
import { webSocket, WebSocketSubject, WebSocketSubjectConfig } from 'rxjs/webSocket';
import { createTauriWsConnection } from './platform-specific/tauri/tauri-websocket-subject';
import { IsTauriEnvironment } from './platform-specific/utils';

@Injectable()
export class WebsocketService {
  constructor(private ngZone: NgZone) { }

  /**
   * createConnection creates a new websocket connection using opts.
   *
   * @param opts Options for the websocket connection.
   */
  createConnection<T>(opts: WebSocketSubjectConfig<T>): WebSocketSubject<T> {
    if (IsTauriEnvironment()) {    
      console.log('[portmaster-api] Running under Tauri - Using Tauri WebSocket');
      return createTauriWsConnection<T>(opts, this.ngZone);
    }

    console.log('[portmaster-api] Running in browser - Using RxJS WebSocket');
    return webSocket<T>(opts);
  }
}