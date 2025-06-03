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
 *   openObserver: { next: () => console.log('Connection opened') },
 *   closeObserver: { next: () => console.log('Connection closed') },
 *   closingObserver: { next: () => console.log('Connection closing') },
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
        
    // Function to establish a WebSocket connection
    const connect = (): void => {
        WebSocket.connect(opts.url)
            .then((ws) => {
                wsConnection = ws;
                console.log(`${LOG_PREFIX} Connection established`);
                
                // Run inside NgZone to ensure Angular detects this change
                ngZone.run(() => {
                    // Notify that connection is open
                    opts.openObserver?.next(undefined as unknown as Event);                    
                    // Send any pending messages
                    while (pendingMessages.length > 0) {
                        const message = pendingMessages.shift();
                        if (message) webSocketSubject.next(message);
                    }
                });

                setupMessageListener(ws);
            })
            .catch((error: Error) => {                
                console.error(`${LOG_PREFIX} Connection failed:`, error);            
                reconnect();
            });
    };
    
    // Function to reconnect
    let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
    const reconnect = () => {
        if (reconnectTimeout) {
            clearTimeout(reconnectTimeout);
        }
        
        // Notify close observer
        ngZone.run(() => {
          opts.closeObserver?.next(undefined as unknown as CloseEvent); 
        })
        
        // Remove the existing listener if it exists
        removeListener();
        
        // Close the existing connection if it exists
        wsConnection?.disconnect().catch(err => console.warn(`${LOG_PREFIX} Error closing connection during reconnect:`, err));
        wsConnection = null;
        
        // Connect again after a delay
        console.log(`${LOG_PREFIX} Attempting to reconnect in 1 second...`);
        reconnectTimeout = setTimeout(() => {
          reconnectTimeout = null;
          connect();
        }, 1000);
    };

    // Function to remove the message listener
    let listenerRemovalFn: (() => void) | null = null; // Store the removal function for the ws listener
    const removeListener = () => {
        if (listenerRemovalFn) {
            try {
                listenerRemovalFn();
                listenerRemovalFn = null; // Clear the reference
            } catch (err) {
                console.error(`${LOG_PREFIX} Error removing listener:`, err);
            }
        }
    }

    // Function to set up the message listener
    const setupMessageListener = (ws: WebSocket) => {        
        let pingTimeoutId: ReturnType<typeof setTimeout> | null = null;
        
        listenerRemovalFn = ws.addListener((message: Message) => {
            // Process message inside ngZone to trigger change detection
            try {
                switch (message.type) {
                    case 'Text':
                      try {
                        const deserializedMessage = deserializer({ data: message.data as string } as any);
                        ngZone.run(() => { messageSubject.next(deserializedMessage); }); // inside ngZone to trigger change detection
                      } catch (err) {
                        console.error(`${LOG_PREFIX} Error deserializing text message:`, err);
                      }
                      break;
                        
                    case 'Binary':
                      try {
                        const uint8Array = new Uint8Array(message.data as number[]);
                        const deserializedMessage = deserializer({ data: uint8Array.buffer } as any);
                        ngZone.run(() => { messageSubject.next(deserializedMessage); }); // inside ngZone to trigger change detection
                      } catch (err) {
                          console.error(`${LOG_PREFIX} Error deserializing binary message:`, err);
                      }
                      break;
                        
                    case 'Close':
                      console.log(`${LOG_PREFIX} Connection closed by server`);
                      reconnect(); // Auto-reconnect on server-initiated close
                      break;
                        
                    case 'Ping':
                      break;

                    case 'Pong':
                      console.log(`${LOG_PREFIX} Received pong response - connection is alive`);                      
                      if (pingTimeoutId) {
                          // Clear the timeout since we got a response
                          clearTimeout(pingTimeoutId);
                          pingTimeoutId = null;
                      }
                      break;

                    // All other message types are unexpected. Proceed with reconnect.
                    default:
                      console.warn(`${LOG_PREFIX} Received unexpected message: '${message}'`);
                      
                      // Don't immediately reconnect - first verify if the connection is actually dead.
                      // If we don't receive a pong response within 2 seconds, we consider the connection dead.
                      if (!pingTimeoutId && wsConnection) {
                          console.log(`${LOG_PREFIX} Verifying connection status with ping...`);
                          wsConnection.send( {type: 'Ping', data: [1]} ).then(() => {
                              pingTimeoutId = setTimeout(() => {
                                  console.error(`${LOG_PREFIX} No response to ping - connection appears dead`);
                                  pingTimeoutId = null;
                                  reconnect();
                              }, 2000);                              
                          }).catch(err => {
                              console.error(`${LOG_PREFIX} Failed to send ping - connection is dead:`, err);
                              reconnect();
                          });
                      }

                      break;
                }
            } catch (error) {
                console.error(`${LOG_PREFIX} Error processing message: `, error);
            }
        });
    };

    //////////////////////////////////////////////////////////////
    // Connect to WebSocket
    //////////////////////////////////////////////////////////////
    console.log(`${LOG_PREFIX} Connecting to WebSocket:`, opts.url);
    
    // Connect outside of Angular zone for better performance
    ngZone.runOutsideAngular(() => {
        connect();
    });

    //////////////////////////////////////////////////////////////
    // RxJS WebSocketSubject-compatible implementation
    //////////////////////////////////////////////////////////////
    const webSocketSubject = {
      // Standard Observer interface methods
      next: (message: T) => {
        // Run outside NgZone for better performance during send operations
        ngZone.runOutsideAngular(() => {   
            // If the connection is not established yet, queue the message
            if (!wsConnection) {
              if (pendingMessages.length >= 100) {
                console.error(`${LOG_PREFIX} Too many pending messages, skipping message`);
                return;
              }
              pendingMessages.push(message);
              console.warn(`${LOG_PREFIX} Connection not established yet, message queued`);
              return;
            }

            // Serialize the message using the provided serializer
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

            // Send the serialized message through the WebSocket connection
            wsConnection?.send(serializedMessage).catch((err: Error) => {
              console.error(`${LOG_PREFIX} Error sending message:`, err);
            });
        });
      },

      complete: () => {
        if (wsConnection) {
          console.log(`${LOG_PREFIX} Closing connection`);
          
          // Run inside NgZone to ensure Angular detects this change
          ngZone.run(() => {
            opts.closingObserver?.next();            
            wsConnection!.disconnect().catch((err: Error) => console.error(`${LOG_PREFIX} Error closing connection:`, err));
            wsConnection = null;
            opts.closeObserver?.next(undefined as unknown as CloseEvent);                        
          });

          // Remove the existing listener if it exists
          removeListener();
        }
        
        messageSubject.complete();        
      },

      // RxJS Observable methods required for compatibility
      pipe: function(): Observable<any> {
        // @ts-ignore - Ignore the parameter type mismatch
        return observable$.pipe(...arguments);
      },
    };

    // Cast to WebSocketSubject<T>
    return webSocketSubject as unknown as WebSocketSubject<T>;
}