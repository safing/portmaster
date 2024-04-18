import { HttpClient, HttpHeaders, HttpResponse } from '@angular/common/http';
import {
  Inject,
  Injectable,
  InjectionToken,
  isDevMode,
  NgZone,
} from '@angular/core';
import { BehaviorSubject, Observable, Observer, of } from 'rxjs';
import {
  concatMap,
  delay,
  filter,
  map,
  retryWhen,
  takeWhile,
  tap,
} from 'rxjs/operators';
import { WebSocketSubject } from 'rxjs/webSocket';
import {
  DataReply,
  deserializeMessage,
  DoneReply,
  ImportResult,
  InspectedActiveRequest,
  isCancellable,
  isDataReply,
  ProfileImportResult,
  Record,
  ReplyMessage,
  Requestable,
  RequestMessage,
  RequestType,
  RetryableOpts,
  retryPipeline,
  serializeMessage,
  WatchOpts,
} from './portapi.types';
import { WebsocketService } from './websocket.service';

export const PORTMASTER_WS_API_ENDPOINT = new InjectionToken<string>(
  'PortmasterWebsocketEndpoint'
);
export const PORTMASTER_HTTP_API_ENDPOINT = new InjectionToken<string>(
  'PortmasterHttpApiEndpoint'
);

export const RECONNECT_INTERVAL = 2000;

let uniqueRequestId = 0;

interface PendingMethod {
  observer: Observer<ReplyMessage>;
  request: RequestMessage;
}

@Injectable()
export class PortapiService {
  /** The actual websocket connection, auto-(re)connects on subscription */
  private ws$: WebSocketSubject<ReplyMessage | RequestMessage> | null;

  /** used to emit changes to our "connection state" */
  private connectedSubject = new BehaviorSubject(false);

  /** A map to multiplex websocket messages to the actual observer/initator */
  private _streams$ = new Map<string, Observer<ReplyMessage<any>>>();

  /** Map to keep track of "still-to-send" requests when we are currently disconnected */
  private _pendingCalls$ = new Map<string, PendingMethod>();

  /** Whether or not we are currently connected. */
  get connected$() {
    return this.connectedSubject.asObservable();
  }

  /** @private DEBUGGING ONLY - keeps track of current requests and supports injecting messages  */
  readonly activeRequests = new BehaviorSubject<{
    [key: string]: InspectedActiveRequest;
  }>({});

  constructor(
    private websocketFactory: WebsocketService,
    private ngZone: NgZone,
    private http: HttpClient,
    @Inject(PORTMASTER_HTTP_API_ENDPOINT) private httpEndpoint: string,
    @Inject(PORTMASTER_WS_API_ENDPOINT) private wsEndpoint: string
  ) {
    // create a new websocket connection that will auto-connect
    // on the first subscription and will automatically reconnect
    // with consecutive subscribers.
    this.ws$ = this.createWebsocket();

    // no need to keep a reference to the subscription as we're not going
    // to unsubscribe ...
    this.ws$
      .pipe(
        retryWhen((errors) =>
          errors.pipe(
            // use concatMap to keep the errors in order and make sure
            // they don't execute in parallel.
            concatMap((e, i) =>
              of(e).pipe(
                // We need to forward the error to all streams here because
                // due to the retry feature the subscriber below won't see
                // any error at all.
                tap(() => {
                  this._streams$.forEach((observer) => observer.error(e));
                  this._streams$.clear();
                }),
                delay(1000)
              )
            )
          )
        )
      )
      .subscribe(
        (msg) => {
          const observer = this._streams$.get(msg.id);
          if (!observer) {
            // it's expected that we receive done messages from time to time here
            // as portmaster sends a "done" message after we "cancel" a subscription
            // and we already remove the observer from _streams$ if the subscription
            // is unsubscribed. So just hide that warning message for "done"
            if (msg.type !== 'done') {
              console.warn(
                `Received message for unknown request id ${msg.id} (type=${msg.type})`,
                msg
              );
            }
            return;
          }

          // forward the message to the actual stream.
          observer.next(msg as ReplyMessage);
        },
        console.error,
        () => {
          // This should actually never happen but if, make sure
          // we handle it ...
          this._streams$.forEach((observer) => observer.complete());
          this._streams$.clear();
        }
      );
  }

  /** Triggers a restart of the portmaster service */
  restartPortmaster(): Observable<any> {
    return this.http.post(`${this.httpEndpoint}/v1/core/restart`, undefined, {
      observe: 'response',
      responseType: 'arraybuffer',
    });
  }

  /** Triggers a shutdown of the portmaster service */
  shutdownPortmaster(): Observable<any> {
    return this.http.post(`${this.httpEndpoint}/v1/core/shutdown`, undefined, {
      observe: 'response',
      responseType: 'arraybuffer',
    });
  }

  /** Force the portmaster to check for updates */
  checkForUpdates(): Observable<any> {
    return this.http.post(`${this.httpEndpoint}/v1/updates/check`, undefined, {
      observe: 'response',
      responseType: 'arraybuffer',
      reportProgress: false,
    });
  }

  /** Force a reload of the UI assets */
  reloadUI(): Observable<any> {
    return this.http.post(`${this.httpEndpoint}/v1/ui/reload`, undefined, {
      observe: 'response',
      responseType: 'arraybuffer',
    });
  }

  /** Clear DNS cache */
  clearDNSCache(): Observable<any> {
    return this.http.post(`${this.httpEndpoint}/v1/dns/clear`, undefined, {
      observe: 'response',
      responseType: 'arraybuffer',
    });
  }

  /** Reset the broadcast notifications state */
  resetBroadcastState(): Observable<any> {
    return this.http.post(
      `${this.httpEndpoint}/v1/broadcasts/reset-state`,
      undefined,
      { observe: 'response', responseType: 'arraybuffer' }
    );
  }

  /** Re-initialize the SPN */
  reinitSPN(): Observable<any> {
    return this.http.post(`${this.httpEndpoint}/v1/spn/reinit`, undefined, {
      observe: 'response',
      responseType: 'arraybuffer',
    });
  }

  /** Cleans up the history database by applying history retention settings */
  cleanupHistory(): Observable<any> {
    return this.http.post(
      `${this.httpEndpoint}/v1/netquery/history/cleanup`,
      undefined,
      { observe: 'response', responseType: 'arraybuffer' }
    );
  }

  /** Requests a resource from the portmaster as application/json and automatically parses the response body*/
  getResource<T>(resource: string): Observable<T>;

  /** Requests a resource from the portmaster as text */
  getResource(resource: string, type: string): Observable<HttpResponse<string>>;

  getResource(
    resource: string,
    type?: string
  ): Observable<HttpResponse<string> | any> {
    if (type !== undefined) {
      return this.http.get(`${this.httpEndpoint}/v1/updates/get/${resource}`, {
        headers: new HttpHeaders({ Accept: type }),
        observe: 'response',
        responseType: 'text',
      });
    }

    return this.http.get<any>(
      `${this.httpEndpoint}/v1/updates/get/${resource}`,
      {
        headers: new HttpHeaders({ Accept: 'application/json' }),
        responseType: 'json',
      }
    );
  }

  /** Export one or more settings, either from global settings or a specific profile */
  exportSettings(
    keys: string[],
    from: 'global' | string = 'global'
  ): Observable<string> {
    return this.http.post(
      `${this.httpEndpoint}/v1/sync/settings/export`,
      {
        from,
        keys,
      },
      {
        headers: new HttpHeaders({ Accept: 'text/yaml' }),
        responseType: 'text',
        observe: 'body',
      }
    );
  }

  /** Validate a settings import for a given target */
  validateSettingsImport(
    blob: string | Blob,
    target: string | 'global' = 'global',
    mimeType: string = 'text/yaml'
  ): Observable<ImportResult> {
    return this.http.post<ImportResult>(
      `${this.httpEndpoint}/v1/sync/settings/import`,
      {
        target,
        rawExport: blob.toString(),
        rawMime: mimeType,
        validateOnly: true,
      }
    );
  }

  /** Import settings into a given target */
  importSettings(
    blob: string | Blob,
    target: string | 'global' = 'global',
    mimeType: string = 'text/yaml',
    reset = false,
    allowUnknown = false
  ): Observable<ImportResult> {
    return this.http.post<ImportResult>(
      `${this.httpEndpoint}/v1/sync/settings/import`,
      {
        target,
        rawExport: blob.toString(),
        rawMime: mimeType,
        validateOnly: false,
        reset,
        allowUnknown,
      }
    );
  }

  /** Import a profile */
  importProfile(
    blob: string | Blob,
    mimeType: string = 'text/yaml',
    reset = false,
    allowUnknown = false,
    allowReplaceProfiles = false
  ): Observable<ImportResult> {
    return this.http.post<ProfileImportResult>(
      `${this.httpEndpoint}/v1/sync/profile/import`,
      {
        rawExport: blob.toString(),
        rawMime: mimeType,
        validateOnly: false,
        reset,
        allowUnknown,
        allowReplaceProfiles,
      }
    );
  }

  /** Import a profile */
  validateProfileImport(
    blob: string | Blob,
    mimeType: string = 'text/yaml'
  ): Observable<ImportResult> {
    return this.http.post<ProfileImportResult>(
      `${this.httpEndpoint}/v1/sync/profile/import`,
      {
        rawExport: blob.toString(),
        rawMime: mimeType,
        validateOnly: true,
      }
    );
  }

  /** Export one or more settings, either from global settings or a specific profile */
  exportProfile(id: string): Observable<string> {
    return this.http.post(
      `${this.httpEndpoint}/v1/sync/profile/export`,
      {
        id,
      },
      {
        headers: new HttpHeaders({ Accept: 'text/yaml' }),
        responseType: 'text',
        observe: 'body',
      }
    );
  }

  /** Merge multiple profiles into one primary profile. */
  mergeProfiles(
    name: string,
    primary: string,
    secondaries: string[]
  ): Observable<string> {
    return this.http
      .post<{ new: string }>(`${this.httpEndpoint}/v1/profile/merge`, {
        name: name,
        to: primary,
        from: secondaries,
      })
      .pipe(map((response) => response.new));
  }

  /**
   * Injects an event into a module to trigger certain backend
   * behavior.
   *
   * @deprecated - Use the HTTP API instead.
   *
   * @param module The name of the module to inject
   * @param kind The event kind to inject
   */
  bridgeAPI(call: string, method: string): Observable<void> {
    return this.create(`api:${call}`, {
      Method: method,
    }).pipe(map(() => { }));
  }

  /**
   * Flushes all pending method calls that have been collected
   * while we were not connected to the portmaster API.
   */
  private _flushPendingMethods() {
    const count = this._pendingCalls$.size;
    try {
      this._pendingCalls$.forEach((req, key) => {
        // It's fine if we throw an error here!
        this.ws$!.next(req.request);
        this._streams$.set(req.request.id, req.observer);
        this._pendingCalls$.delete(key);
      });
    } catch (err) {
      // we failed to send the pending calls because the
      // websocket connection just broke.
      console.error(
        `Failed to flush pending calls, ${this._pendingCalls$.size} left: `,
        err
      );
    }

    console.log(`Successfully flushed all (${count}) pending calles`);
  }

  /**
   * Allows to inspect currently active requests.
   */
  inspectActiveRequests(): { [key: string]: InspectedActiveRequest } {
    return this.activeRequests.getValue();
  }

  /**
   * Loads a database entry. The returned observable completes
   * after the entry has been loaded.
   *
   * @param key The database key of the entry to load.
   */
  get<T extends Record>(key: string): Observable<T> {
    return this.request('get', { key }).pipe(map((res) => res.data));
  }

  /**
   * Searches for multiple database entries at once. Each entry
   * is streams via the returned observable. The observable is
   * closed after the last entry has been published.
   *
   * @param query The query used to search the database.
   */
  query<T extends Record>(query: string): Observable<DataReply<T>> {
    return this.request('query', { query });
  }

  /**
   * Subscribes for updates on entries of the selected query.
   *
   * @param query The query use to subscribe.
   */
  sub<T extends Record>(
    query: string,
    opts: RetryableOpts = {}
  ): Observable<DataReply<T>> {
    return this.request('sub', { query }).pipe(retryPipeline(opts));
  }

  /**
   * Subscribes for updates on entries of the selected query and
   * ensures entries are stream once upon subscription.
   *
   * @param query The query use to subscribe.
   * @todo(ppacher): check what a ok/done message mean here.
   */
  qsub<T extends Record>(
    query: string,
    opts?: RetryableOpts
  ): Observable<DataReply<T>>;
  qsub<T extends Record>(
    query: string,
    opts: RetryableOpts,
    _: { forwardDone: true }
  ): Observable<DataReply<T> | DoneReply>;
  qsub<T extends Record>(
    query: string,
    opts: RetryableOpts = {},
    { forwardDone }: { forwardDone?: true } = {}
  ): Observable<DataReply<T>> {
    return this.request('qsub', { query }, { forwardDone }).pipe(
      retryPipeline(opts)
    );
  }

  /**
   * Creates a new database entry.
   *
   * @warn create operations do not validate the type of data
   * to be overwritten (for keys that does already exist).
   * Use {@function insert} for more validation.
   *
   * @param key The database key for the entry.
   * @param data The actual data for the entry.
   */
  create(key: string, data: any): Observable<void> {
    data = this.stripMeta(data);
    return this.request('create', { key, data }).pipe(map(() => { }));
  }

  /**
   * Updates an existing entry.
   *
   * @param key The database key for the entry
   * @param data The actual, updated entry data.
   */
  update(key: string, data: any): Observable<void> {
    data = this.stripMeta(data);
    return this.request('update', { key, data }).pipe(map(() => { }));
  }

  /**
   * Creates a new database entry.
   *
   * @param key The database key for the entry.
   * @param data The actual data for the entry.
   * @todo(ppacher): check what's different to create().
   */
  insert(key: string, data: any): Observable<void> {
    data = this.stripMeta(data);
    return this.request('insert', { key, data }).pipe(map(() => { }));
  }

  /**
   * Deletes an existing database entry.
   *
   * @param key The key of the database entry to delete.
   */
  delete(key: string): Observable<void> {
    return this.request('delete', { key }).pipe(map(() => { }));
  }

  /**
   * Watch a database key for modifications. If the
   * websocket connection is lost or an error is returned
   * watch will automatically retry after retryDelay
   * milliseconds. It stops retrying to watch key once
   * maxRetries is exceeded. The returned observable completes
   * when the watched key is deleted.
   *
   * @param key The database key to watch
   * @param opts.retryDelay Number of milliseconds to wait
   *        between retrying the request. Defaults to 1000
   * @param opts.maxRetries Maximum number of tries before
   *        giving up. Defaults to Infinity
   * @param opts.ingoreNew Whether or not `new` notifications
   *        will be ignored. Defaults to false
   * @param opts.ignoreDelete Whether or not "delete" notification
   *        will be ignored (and replaced by null)
   * @param forwardDone: Whether or not the "done" message should be forwarded
   */
  watch<T extends Record>(key: string, opts?: WatchOpts): Observable<T>;
  watch<T extends Record>(
    key: string,
    opts?: WatchOpts & { ignoreDelete: true }
  ): Observable<T | null>;
  watch<T extends Record>(
    key: string,
    opts: WatchOpts,
    _: { forwardDone: true }
  ): Observable<T | DoneReply>;
  watch<T extends Record>(
    key: string,
    opts: WatchOpts & { ignoreDelete: true },
    _: { forwardDone: true }
  ): Observable<T | DoneReply | null>;
  watch<T extends Record>(
    key: string,
    opts: WatchOpts = {},
    { forwardDone }: { forwardDone?: boolean } = {}
  ): Observable<T | DoneReply | null> {
    return this.qsub<T>(key, opts, { forwardDone } as any).pipe(
      filter((reply) => reply.type !== 'done' || forwardDone === true),
      filter((reply) => reply.type === 'done' || reply.key === key),
      takeWhile((reply) => opts.ignoreDelete || reply.type !== 'del'),
      filter((reply) => {
        return !opts.ingoreNew || reply.type !== 'new';
      }),
      map((reply) => {
        if (reply.type === 'del') {
          return null;
        }

        if (reply.type === 'done') {
          return reply;
        }
        return reply.data;
      })
    );
  }

  watchAll<T extends Record>(
    query: string,
    opts?: RetryableOpts
  ): Observable<T[]> {
    return new Observable<T[]>((observer) => {
      let values: T[] = [];
      let keys: string[] = [];
      let doneReceived = false;

      const sub = this.request(
        'qsub',
        { query },
        { forwardDone: true }
      ).subscribe({
        next: (value) => {
          if ((value as any).type === 'done') {
            doneReceived = true;
            observer.next(values);
            return;
          }

          if (!doneReceived) {
            values.push(value.data);
            keys.push(value.key);
            return;
          }

          const idx = keys.findIndex((k) => k === value.key);
          switch (value.type) {
            case 'new':
              if (idx < 0) {
                values.push(value.data);
                keys.push(value.key);
              } else {
                /*
                                    const existing = values[idx]._meta!;
                                    const existingTs = existing.Modified || existing.Created;
                                    const newTs = (value.data as Record)?._meta?.Modified || (value.data as Record)?._meta?.Created || 0;

                                    console.log(`Comparing ${newTs} against ${existingTs}`);

                                    if (newTs > existingTs) {
                                      console.log(`New record is ${newTs - existingTs} seconds newer`);
                                      values[idx] = value.data;
                                    } else {
                                      return;
                                    }
                  */
                values[idx] = value.data;
              }
              break;
            case 'del':
              if (idx >= 0) {
                keys.splice(idx, 1);
                values.splice(idx, 1);
              }
              break;
            case 'upd':
              if (idx >= 0) {
                values[idx] = value.data;
              }
              break;
          }

          observer.next(values);
        },
        error: (err) => {
          observer.error(err);
        },
        complete: () => {
          observer.complete();
        },
      });

      return () => {
        sub.unsubscribe();
      };
    }).pipe(retryPipeline(opts));
  }

  /**
   * Close the current websocket connection. A new subscription
   * will _NOT_ trigger a reconnect.
   */
  close() {
    if (!this.ws$) {
      return;
    }

    this.ws$.complete();
    this.ws$ = null;
  }

  request<M extends RequestType, R extends Record = any>(
    method: M,
    attrs: Partial<Requestable<M>>,
    { forwardDone }: { forwardDone?: boolean } = {}
  ): Observable<DataReply<R>> {
    return new Observable((observer) => {
      const id = `${++uniqueRequestId}`;
      if (!this.ws$) {
        observer.error('No websocket connection');
        return;
      }

      let shouldCancel = isCancellable(method);
      let unsub: () => RequestMessage | null = () => {
        if (shouldCancel) {
          return {
            id: id,
            type: 'cancel',
          };
        }

        return null;
      };

      const request: any = {
        ...attrs,
        id: id,
        type: method,
      };

      let inspected: InspectedActiveRequest = {
        type: method,
        messagesReceived: 0,
        observer: observer,
        payload: request,
        lastData: null,
        lastKey: '',
      };

      if (isDevMode()) {
        this.activeRequests.next({
          ...this.inspectActiveRequests(),
          [id]: inspected,
        });
      }

      let stream$: Observable<ReplyMessage<any>> = this.multiplex(
        request,
        unsub
      );
      if (isDevMode()) {
        // in development mode we log all replys for the different
        // methods. This also includes updates to subscriptions.
        stream$ = stream$.pipe(
          tap(
            (msg) => { },
            //msg => console.log(`[portapi] reply for ${method} ${id}: `, msg),
            (err) => console.error(`[portapi] error in ${method} ${id}: `, err)
          )
        );
      }

      const subscription = stream$?.subscribe({
        next: (data) => {
          inspected.messagesReceived++;

          // in all cases, an `error` message type
          // terminates the data flow.
          if (data.type === 'error') {
            console.error(data.message, inspected);
            shouldCancel = false;

            observer.error(data.message);
            return;
          }

          if (
            method === 'create' ||
            method === 'update' ||
            method === 'insert' ||
            method === 'delete'
          ) {
            // for data-manipulating methods success
            // ends the stream.
            if (data.type === 'success') {
              observer.next();
              observer.complete();
              return;
            }
          }

          if (method === 'query' || method === 'sub' || method === 'qsub') {
            if (data.type === 'warning') {
              console.warn(data.message);
              return;
            }

            // query based methods send `done` once all
            // results are sent at least once.
            if (data.type === 'done') {
              if (method === 'query') {
                // done ends the query but does not end sub or qsub
                shouldCancel = false;
                observer.complete();
                return;
              }

              if (!!forwardDone) {
                // A done message in qsub does not actually represent
                // a DataReply but we still want to forward that.
                observer.next(data as any);
              }
              return;
            }
          }

          if (!isDataReply(data)) {
            console.error(
              `Received unexpected message type ${data.type} in a ${method} operation`
            );
            return;
          }

          inspected.lastData = data.data;
          inspected.lastKey = data.key;

          observer.next(data);

          // for a `get` method the first `ok` message
          // also marks the end of the stream.
          if (method === 'get' && data.type === 'ok') {
            shouldCancel = false;
            observer.complete();
          }
        },
        error: (err) => {
          console.error(err, attrs);
          observer.error(err);
        },
        complete: () => {
          observer.complete();
        },
      });

      if (isDevMode()) {
        // make sure we remove the "active" request when the subscription
        // goes down
        subscription.add(() => {
          const active = this.inspectActiveRequests();
          delete active[request.id];
          this.activeRequests.next(active);
        });
      }

      return () => {
        subscription.unsubscribe();
      };
    });
  }

  private multiplex(
    req: RequestMessage,
    cancel: (() => RequestMessage | null) | null
  ): Observable<ReplyMessage> {
    return new Observable((observer) => {
      if (this.connectedSubject.getValue()) {
        // Try to directly send the request to the backend
        this._streams$.set(req.id, observer);
        this.ws$!.next(req);
      } else {
        // in case of an error we just add the request as
        // "pending" and wait for the connection to be
        // established.
        console.warn(
          `Failed to send request ${req.id}:${req.type}, marking as pending ...`
        );
        this._pendingCalls$.set(req.id, {
          request: req,
          observer: observer,
        });
      }

      return () => {
        // Try to cancel the request but ingore
        // any errors here.
        try {
          if (cancel !== null) {
            const cancelMsg = cancel();
            if (!!cancelMsg) {
              this.ws$!.next(cancelMsg);
            }
          }
        } catch (err) { }

        this._pendingCalls$.delete(req.id);
        this._streams$.delete(req.id);
      };
    });
  }

  /**
   * Inject a message into a PortAPI stream.
   *
   * @param id The request ID to inject msg into.
   * @param msg The message to inject.
   */
  _injectMessage(id: string, msg: DataReply<any>) {
    // we are using runTask here so change-detection is
    // triggered as needed
    this.ngZone.runTask(() => {
      const req = this.activeRequests.getValue()[id];
      if (!req) {
        return;
      }

      req.observer.next(msg as DataReply<any>);
    });
  }

  /**
   * Injects a 'ok' type message
   *
   * @param id The ID of the request to inject into
   * @param data The data blob to inject
   * @param key [optional] The key of the entry to inject
   */
  _injectData(id: string, data: any, key: string = '') {
    this._injectMessage(id, { type: 'ok', data: data, key, id: id });
  }

  /**
   * Patches the last message received on id by deeply merging
   * data and re-injects that message.
   *
   * @param id The ID of the request
   * @param data The patch to apply and reinject
   */
  _patchLast(id: string, data: any) {
    const req = this.activeRequests.getValue()[id];
    if (!req || !req.lastData) {
      return;
    }

    const newPayload = mergeDeep({}, req.lastData, data);
    this._injectData(id, newPayload, req.lastKey);
  }

  private stripMeta<T extends Record>(obj: T): T {
    let copy = {
      ...obj,
      _meta: undefined,
    };
    return copy;
  }

  /**
   * Creates a new websocket subject and configures appropriate serializer
   * and deserializer functions for PortAPI.
   *
   * @private
   */
  private createWebsocket(): WebSocketSubject<ReplyMessage | RequestMessage> {
    return this.websocketFactory.createConnection<
      ReplyMessage | RequestMessage
    >({
      url: this.wsEndpoint,
      serializer: (msg) => {
        try {
          return serializeMessage(msg);
        } catch (err) {
          console.error('serialize message', err);
          return {
            type: 'error',
          };
        }
      },
      // deserializeMessage also supports RequestMessage so cast as any
      deserializer: <any>((msg: any) => {
        try {
          const res = deserializeMessage(msg);
          return res;
        } catch (err) {
          console.error('deserialize message', err);
          return {
            type: 'error',
          };
        }
      }),
      binaryType: 'arraybuffer',
      openObserver: {
        next: () => {
          console.log('[portapi] connection to portmaster established');
          this.connectedSubject.next(true);
          this._flushPendingMethods();
        },
      },
      closeObserver: {
        next: () => {
          console.log('[portapi] connection to portmaster closed');
          this.connectedSubject.next(false);
        },
      },
      closingObserver: {
        next: () => {
          console.log('[portapi] connection to portmaster closing');
        },
      },
    });
  }
}

// Counts the number of "truthy" datafields in obj.
function countTruthyDataFields(obj: { [key: string]: any }): number {
  let count = 0;
  Object.keys(obj).forEach((key) => {
    let value = obj[key];
    if (!!value) {
      count++;
    }
  });
  return count;
}

function isObject(item: any): item is Object {
  return item && typeof item === 'object' && !Array.isArray(item);
}

export function mergeDeep(target: any, ...sources: any): any {
  if (!sources.length) return target;
  const source = sources.shift();

  if (isObject(target) && isObject(source)) {
    for (const key in source) {
      if (isObject(source[key])) {
        if (!target[key]) Object.assign(target, { [key]: {} });
        mergeDeep(target[key], source[key]);
      } else {
        Object.assign(target, { [key]: source[key] });
      }
    }
  }

  return mergeDeep(target, ...sources);
}
