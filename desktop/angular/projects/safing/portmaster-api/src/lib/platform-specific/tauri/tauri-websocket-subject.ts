import WebSocket, { ConnectionConfig, Message } from '@tauri-apps/plugin-websocket';
import { Subject, Observable, merge, mergeMap, throwError } from 'rxjs';
import { WebSocketSubject, WebSocketSubjectConfig } from 'rxjs/webSocket';
import { NgZone } from '@angular/core';

const LOG_PREFIX        = '[tauri_ws]';
const PING_INTERVAL_MS  = 10000;  // Send a ping every PING_INTERVAL_MS milliseconds
const PONG_TIMEOUT_MS   = 5000;   // Wait PONG_TIMEOUT_MS milliseconds for a pong response
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
    const errorSubject   = new Subject<any>(); // Added for error propagation

    // Combined stream with both messages and errors
    const observable$ = merge(
      messageSubject.asObservable(),
      errorSubject.pipe(
        mergeMap(err => throwError(() => err))
      )
    );

    //////////////////////////////////////////////////////////////
    // Track subscriptions
    //////////////////////////////////////////////////////////////
    let subscriptionCount = 0;

    // Wrapper with subscription tracking
    const trackedObservable$ = new Observable<T>(subscriber => {
      subscriptionCount++;

      // If this is the first subscription, connect to WebSocket
      if (subscriptionCount === 1) {
        ngZone.runOutsideAngular(() => {
          connect();
        });
      }

      const subscription = observable$.subscribe({
        next: value => subscriber.next(value),
        error: err => subscriber.error(err),
        complete: () => subscriber.complete()
      });
      
      // Cleanup function - called when unsubscribed
      return () => {
        subscriptionCount--;        
        subscription.unsubscribe();

        // If this was the last subscription, close the WebSocket connection
        if (subscriptionCount === 0) {
          disconnect();
        } 
      };
    });
    
    //////////////////////////////////////////////////////////////
    // Function to establish a WebSocket connection
    //////////////////////////////////////////////////////////////
    let listenerRemovalFn: (() => void) | null = null; // Store the removal function for the ws listener

    const connect = (): void => {
      console.log(`${LOG_PREFIX} Connecting to WebSocket: ${opts.url}`);
      WebSocket.connect(opts.url)
          .then((ws) => {
              wsConnection = ws;
              console.log(`${LOG_PREFIX} Connection established`);
              opts.openObserver?.next(undefined as unknown as Event);
              listenerRemovalFn = ws.addListener(messagesListener);
              startHealthChecks();
          })
          .catch((error: Error) => {                
              console.error(`${LOG_PREFIX} Connection failed:`, error);
              errorSubject.next(error);
          });
    };

    const disconnect = (): void => {
      stopHealthChecks();
      
      if (listenerRemovalFn) {
        try {
            listenerRemovalFn();            
        } catch (err) {
            console.error(`${LOG_PREFIX} Error removing listener:`, err);
        }
        listenerRemovalFn = null; // Clear the reference
      }

      const currentWs = wsConnection;
      wsConnection = null;

      if (!currentWs) return;

      console.log(`${LOG_PREFIX} Closing WebSocket connection.`);
      opts.closeObserver?.next(undefined as unknown as CloseEvent);
      currentWs.disconnect().catch(err => console.warn(`${LOG_PREFIX} Error closing connection:`, err));
    }

    //////////////////////////////////////////////////////////////
    // Function to check if connection alive
    //////////////////////////////////////////////////////////////    
    let healthCheckIntervalId: ReturnType<typeof setInterval> | null = null;
    let pongTimeoutId: ReturnType<typeof setTimeout> | null = null;

    const startHealthChecks = () => {
        stopHealthChecks(); // Ensure no multiple intervals are running
        healthCheckIntervalId = setInterval(() => {
            if (!wsConnection) {
                stopHealthChecks();
                return;
            }
            if (pongTimeoutId) {
                // Ping already in flight, waiting for pong.
                return;
            }

            wsConnection.send({ type: 'Ping', data: [] })
                .then(() => {
                    pongTimeoutId = setTimeout(() => {
                        console.error(`${LOG_PREFIX} No Pong received. Connection is likely dead.`);
                        errorSubject.next(new Error('Connection timed out'));
                        stopHealthChecks();
                    }, PONG_TIMEOUT_MS);
                })
                .catch(err => {
                    console.error(`${LOG_PREFIX} Ping send failed:`, err);
                    errorSubject.next(new Error(`Ping send failed: ${err}`));
                    stopHealthChecks();
                });
        }, PING_INTERVAL_MS);
    };

    const stopHealthChecks = () => {
        if (healthCheckIntervalId) {
            clearInterval(healthCheckIntervalId);
            healthCheckIntervalId = null;
        }
        if (pongTimeoutId) {
            clearTimeout(pongTimeoutId);
            pongTimeoutId = null;
        }
    };

    //////////////////////////////////////////////////////////////
    // Messages listener
    //////////////////////////////////////////////////////////////
    const messagesListener = (message: Message) => {
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
            console.warn(`${LOG_PREFIX} Connection closed by server: ${message}`); 
            errorSubject.next(new Error(`Connection closed by server: ${message}`)); 
            break;
              
          case 'Ping':
            break;

          case 'Pong':
            // Pong received, clear the timeout.                     
            if (pongTimeoutId) {
                clearTimeout(pongTimeoutId);
                pongTimeoutId = null;
            }
            break;

          // All other message types are unexpected. Proceed with reconnect.
          default:
            console.warn(`${LOG_PREFIX} Received unexpected message: '${message}'`);                      
            break;
        }
      } catch (error) {
          console.error(`${LOG_PREFIX} Error processing message: `, error);
      }
    }
    
    //////////////////////////////////////////////////////////////
    // RxJS WebSocketSubject-compatible interface
    //////////////////////////////////////////////////////////////
    const webSocketSubject = {
      asObservable: () => trackedObservable$,
      next: (message: T) => {
        // Run outside NgZone for better performance during send operations
        ngZone.runOutsideAngular(() => {   
            if (!wsConnection) {
              errorSubject.next(new Error('Connection not established'));
              return;              
            }                       
            try {
              const serializedMessage = serializer(message);          
              // 'string' type is enough here, since default serializer for portmaster message returns string
              if (typeof serializedMessage !== 'string') 
                throw new Error('Serialized message is not a string');

              wsConnection?.send(serializedMessage).catch((err: Error) => {
                console.error(`${LOG_PREFIX} Error sending message:`, err);
                errorSubject.next(err);
              });
            } catch (error) {
              console.error(`${LOG_PREFIX} Error serializing message:`, error);
              return;
            }
        });
      },
      complete: () => {
        if (wsConnection) {
          opts.closingObserver?.next();       
          disconnect();
        }        
        messageSubject.complete();        
      },
      subscribe: trackedObservable$.subscribe.bind(trackedObservable$),
      pipe: trackedObservable$.pipe.bind(trackedObservable$),      
    };

    return webSocketSubject as unknown as WebSocketSubject<T>;
}