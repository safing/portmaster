import WebSocket, { ConnectionConfig, Message } from '@tauri-apps/plugin-websocket';
import { Subject, Observable } from 'rxjs';
import { WebSocketSubject, WebSocketSubjectConfig } from 'rxjs/webSocket';
import { NgZone } from '@angular/core';

const LOG_PREFIX = '[tauri_ws]';

/**
 * Creates a WebSocket connection using the Tauri WebSocket API and wraps it in an RxJS WebSocketSubject-compatible interface.
 *
 * @template T - The type of messages sent and received through the WebSocket.
 * @param {WebSocketSubjectConfig<T>} opts - Configuration options for the WebSocket connection.
 * @param {NgZone} ngZone - Angular's NgZone to ensure change detection runs properly.
 * @returns {WebSocketSubject<T>} - An RxJS WebSocketSubject-compatible object for interacting with the WebSocket.
 * @throws {Error} If the `serializer` or `deserializer` functions are not provided.
 *
 * @example
 * const wsSubject = createTauriWsConnection({
 *   url: 'ws://example.com',
 *   serializer: JSON.stringify,
 *   deserializer: JSON.parse,
 * }, ngZone);
 */
export function createTauriWsConnection<T>(opts: WebSocketSubjectConfig<T>, ngZone: NgZone): WebSocketSubject<T> {
    if (!opts.serializer)   throw new Error(`${LOG_PREFIX} Messages Serializer not provided!`);
    if (!opts.deserializer) throw new Error(`${LOG_PREFIX} Messages Deserializer not provided!`);
    
    const serializer = opts.serializer;
    const deserializer = opts.deserializer;
    
    let wsConnection: WebSocket | null = null;
    const messageSubject = new Subject<T>();
    const observable$ = messageSubject.asObservable();
    
    // A queue for messages that need to be sent before the connection is established
    const pendingMessages: T[] = [];
        
    const notifySubjectError = (descriptionToLog: string, error: Error | any | null = null) => {
      if (!descriptionToLog) return;
      if (!error)  error = new Error(descriptionToLog);
      console.error(`${LOG_PREFIX} ${descriptionToLog}:`, error);
      
      // Run inside NgZone to ensure Angular detects this change
      ngZone.run(() => {
        // This completes the observable and prevents further messages from being processed.
        messageSubject.error(error);
      });
    }

    //////////////////////////////////////////////////////////////
    // RxJS WebSocketSubject-compatible implementation
    //////////////////////////////////////////////////////////////
    const webSocketSubject = {
      // Standard Observer interface methods
      next: (message: T) => {
        if (!wsConnection) {
          if (pendingMessages.length >= 1000) {
            console.error(`${LOG_PREFIX} Too many pending messages, skipping message`);
            return;
          }
          pendingMessages.push(message);
          console.log(`${LOG_PREFIX} Connection not established yet, message queued`);
          return;
        }

        let serializedMessage: any;
        try {
          serializedMessage = serializer(message);          
          // 'string' type is enough here, since default serializer for portmaster message returns string
          if (typeof serializedMessage !== 'string') 
            throw new Error('Serialized message is not a string');          
        } catch (error) {
          console.error(`${LOG_PREFIX} Error serializing message:`, error);
          return;
        }
        
        // Run outside NgZone for better performance during send operations
        ngZone.runOutsideAngular(() => {
          try { 
            wsConnection!.send(serializedMessage).catch((err: Error) => {
              notifySubjectError('Error sending text message', err);
            });
          } catch (error) {
            notifySubjectError('Error sending message', error);
          }
        });
      },

      complete: () => {
        if (wsConnection) {
          console.log(`${LOG_PREFIX} Closing connection`);
          
          // Run inside NgZone to ensure Angular detects this change
          ngZone.run(() => {
            if (opts.closingObserver?.next) {
              opts.closingObserver.next(undefined);
            }
            
            wsConnection!.disconnect().catch((err: Error) => console.error(`${LOG_PREFIX} Error closing connection:`, err));
            wsConnection = null;
            messageSubject.complete();
          });
        } else {
          messageSubject.complete();
        }
      },

      // RxJS Observable methods required for compatibility
      pipe: function(): Observable<any> {
        // @ts-ignore - Ignore the parameter type mismatch
        return observable$.pipe(...arguments);
      },
    };

    //////////////////////////////////////////////////////////////
    // Connect to WebSocket
    //////////////////////////////////////////////////////////////
    console.log(`${LOG_PREFIX} Connecting to WebSocket:`, opts.url);
    
    // Connect outside of Angular zone for better performance
    ngZone.runOutsideAngular(() => {
      WebSocket.connect(opts.url)
      .then((ws) => {
        wsConnection = ws;
        console.log(`${LOG_PREFIX} Connection established`);
        
        // Run inside NgZone to ensure Angular detects this connection event
        ngZone.run(() => {
          // Create a mock Event for the openObserver
          if (opts.openObserver) {
            const mockEvent = new Event('open') as Event;
            opts.openObserver.next(mockEvent);
          }
          
          // Send any pending messages
          while (pendingMessages.length > 0) {
            const message = pendingMessages.shift();
            if (message) webSocketSubject.next(message);
          }
        });

        try {
          // Add a single listener for ALL message types according to Tauri WebSocket API
          ws.addListener((message: Message) => {
            // Process message inside ngZone to trigger change detection
            ngZone.run(() => {
              try {
                // Handle different message types from Tauri
                switch (message.type) {
                  case 'Text':
                    const textData = message.data as string;
                    try {
                      const deserializedMessage = deserializer({ data: textData } as any);
                      messageSubject.next(deserializedMessage);
                    } catch (err) {
                      notifySubjectError('Error deserializing text message', err);
                    }
                    break;
                    
                  case 'Binary':
                    const binaryData = message.data as number[];
                    try {
                      const uint8Array = new Uint8Array(binaryData);
                      const buffer = uint8Array.buffer;
                      const deserializedMessage = deserializer({ data: buffer } as any);
                      messageSubject.next(deserializedMessage);
                    } catch (err) {
                      notifySubjectError('Error deserializing binary message', err);
                    }
                    break;
                    
                  case 'Close':
                    // Handle close message
                    const closeData = message.data as { code: number; reason: string } | null;
                    console.log(`${LOG_PREFIX} Connection closed by server`, closeData);

                    if (opts.closeObserver) {
                      const closeEvent = {
                        code: closeData?.code || 1000,
                        reason: closeData?.reason || '',
                        wasClean: true,
                        type: 'close',
                        target: null
                      } as unknown as CloseEvent;

                      opts.closeObserver.next(closeEvent);
                    }
                    
                    messageSubject.complete();
                    wsConnection = null;
                    break;
                    
                  case 'Ping':
                    console.log(`${LOG_PREFIX} Received ping`);
                    break;
                    
                  case 'Pong':
                    console.log(`${LOG_PREFIX} Received pong`);
                    break;
                }
              } catch (error) {
                console.error(`${LOG_PREFIX} Error processing message:`, error);
                // Don't error the subject on message processing errors to keep connection alive
              }
            });
          });
          
          console.log(`${LOG_PREFIX} Listener added successfully`);

        } catch (error) {
          notifySubjectError('Error adding message listener', error);
        }
      })
      .catch((error: Error) => {
        notifySubjectError('Connection failed', error);
      });
    });

    // Cast to WebSocketSubject<T>
    return webSocketSubject as unknown as WebSocketSubject<T>;
}