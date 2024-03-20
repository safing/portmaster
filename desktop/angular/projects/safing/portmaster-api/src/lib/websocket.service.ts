import { Injectable } from '@angular/core';
import { webSocket, WebSocketSubject, WebSocketSubjectConfig } from 'rxjs/webSocket';

@Injectable()
export class WebsocketService {
  constructor() { }

  /**
   * createConnection creates a new websocket connection using opts.
   *
   * @param opts Options for the websocket connection.
   */
  createConnection<T>(opts: WebSocketSubjectConfig<T>): WebSocketSubject<T> {
    return webSocket(opts);
  }
}

