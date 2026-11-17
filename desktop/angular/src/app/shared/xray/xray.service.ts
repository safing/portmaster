import { HttpClient } from '@angular/common/http';
import { Inject, Injectable } from '@angular/core';
import { PORTMASTER_HTTP_API_ENDPOINT } from '@safing/portmaster-api';
import { BehaviorSubject, Observable, shareReplay, switchMap, tap } from 'rxjs';
import { XrayNode, XrayStatus, XraySubscription } from './xray.types';

/**
 * Frontend contract for the Xray backend. The backend exposes:
 *   GET  xray/subscriptions
 *   POST xray/subscriptions   (add a URL)
 *   GET  xray/nodes
 *   POST xray/connect
 *   GET  xray/status
 *
 * All paths are relative to the Portmaster HTTP API endpoint and use the
 * same /v1 prefix as the other API services in this application.
 */
@Injectable({ providedIn: 'root' })
export class XrayService {
  private readonly basePath: string;
  private readonly reload$ = new BehaviorSubject<void>(undefined);

  readonly subscriptions$: Observable<XraySubscription[]> = this.reload$.pipe(
    switchMap(() => this.http.get<XraySubscription[]>(`${this.basePath}/subscriptions`)),
  );

  readonly nodes$: Observable<XrayNode[]> = this.reload$.pipe(
    switchMap(() => this.http.get<XrayNode[]>(`${this.basePath}/nodes`)),
  );

  readonly status$: Observable<XrayStatus> = this.reload$.pipe(
    switchMap(() => this.http.get<XrayStatus>(`${this.basePath}/status`)),
    // The page subscribes to status$ in both the template and ngOnInit;
    // share the HTTP result instead of issuing two requests per refresh.
    shareReplay({ bufferSize: 1, refCount: true }),
  );

  constructor(
    private readonly http: HttpClient,
    @Inject(PORTMASTER_HTTP_API_ENDPOINT) httpAPI: string,
  ) {
    this.basePath = `${httpAPI}/v1/xray`;
  }

  /** Add a subscription by URL. */
  addSubscription(url: string): Observable<XraySubscription> {
    return this.http.post<XraySubscription>(`${this.basePath}/subscriptions`, { url })
      .pipe(tap(() => this.refresh()));
  }

  /** Remove a subscription by id. */
  removeSubscription(id: string): Observable<unknown> {
    return this.http.delete(`${this.basePath}/subscriptions/${encodeURIComponent(id)}`)
      .pipe(tap(() => this.refresh()));
  }

  /** Connect to a single node from any subscription. */
  connect(nodeId: string): Observable<unknown> {
    return this.http.post(`${this.basePath}/connect`, { id: nodeId, nodeId })
      .pipe(tap(() => this.refresh()));
  }

  /** Ask the backend to re-fetch all subscription URLs. */
  refresh(): void {
    this.reload$.next();
  }
}
