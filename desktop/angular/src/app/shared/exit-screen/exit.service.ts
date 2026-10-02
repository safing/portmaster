import { IntegrationService } from './../../integration/integration';
import { Injectable, inject } from '@angular/core';
import { PortapiService } from '@safing/portmaster-api';
import { SfngDialogService } from '@safing/ui';
import { BehaviorSubject, merge, of } from 'rxjs';
import { catchError, debounceTime, distinctUntilChanged, map, skip, switchMap, tap, timeout } from 'rxjs/operators';
import { UIStateService } from 'src/app/services';
import { ActionIndicatorService } from '../action-indicator';
import { ExitScreenComponent } from './exit-screen';
import { INTEGRATION_SERVICE } from 'src/app/integration';

const MessageConnecting = '正在连接 Portmaster';
const MessageShutdown = '正在关闭 Portmaster';
const MessageRestart = '正在重启 Portmaster';
const MessageHidden = '';

export type OverlayMessage = typeof MessageConnecting
  | typeof MessageShutdown
  | typeof MessageRestart
  | typeof MessageHidden;

@Injectable({ providedIn: 'root' })
export class ExitService {
  private integration = inject(INTEGRATION_SERVICE);

  private hasOverlay = false;

  private _showOverlay = new BehaviorSubject<OverlayMessage>(MessageConnecting);

  /**
   * Emits whenever the "Connecting to ..." or "Restarting ..." overlays
   * should be shown. It actually emits the message that should be shown.
   * An empty string indicates the overlay should be closed.
   */
  get showOverlay$() { return this._showOverlay.asObservable() }

  constructor(
    private stateService: UIStateService,
    private portapi: PortapiService,
    private dialog: SfngDialogService,
    private uai: ActionIndicatorService,
  ) {

    this.portapi.connected$
      .pipe(
        distinctUntilChanged(),
      )
      .subscribe(connected => {
        if (connected) {
          this._showOverlay.next(MessageHidden);
        } else if (this._showOverlay.getValue() !== MessageShutdown) {
          this._showOverlay.next(MessageConnecting)
        }
      })


    let restartInProgress = false;
    merge<OverlayMessage[]>(
      this.portapi.sub('runtime:modules/core/event/shutdown')
        .pipe(map(() => MessageShutdown)),
      this.portapi.sub('runtime:modules/core/event/restart')
        .pipe(
          tap(() => restartInProgress = true),
          map(() => MessageRestart)
        ),
    )
      .pipe(
        tap(msg => this._showOverlay.next(msg)),
        switchMap(() => this.portapi.connected$),
        distinctUntilChanged(),
        skip(1),
        debounceTime(1000), // make sure we display the "shutdown" overlay for at least a second
      )
      .subscribe(connected => {
        if (this._showOverlay.getValue() === MessageShutdown) {
          setTimeout(() => {
            this.integration.exitApp();
          }, 1000)
        }

        if (connected && restartInProgress) {
          restartInProgress = false;
          this.portapi.reloadUI()
            .pipe(
              tap(() => {
                setTimeout(() => window.location.reload(), 1000)
              })
            )
            .subscribe(this.uai.httpObserver(
              '正在重新加载界面……',
              '重新加载界面失败',
            ))
        }
      })

    window.addEventListener('beforeunload', () => {
      // best effort. may not work all the time depending on
      // the current websocket buffer state
      this.portapi.bridgeAPI('ui/reload', 'POST').subscribe();
    })

    this.integration.onExitRequest(() => {
      this.stateService.uiState()
        // make sure to not wait for the portmaster to start
        .pipe(timeout(1000), catchError(() => of(null)))
        .subscribe(state => {
          if (state?.hideExitScreen) {
            this.integration.exitApp();
            return
          }

          if (this.hasOverlay) {
            return;
          }
          this.hasOverlay = true;

          this.dialog.create(ExitScreenComponent, { autoclose: true })
            .onAction('exit', () => this.integration.exitApp())
            .onClose.subscribe(() => this.hasOverlay = false);
        })
    })
  }

  shutdownPortmaster() {
    this.dialog.confirm({
      canCancel: true,
      header: '关闭 Portmaster',
      message: '关闭 Portmaster 将停止所有 Portmaster 组件，您的系统将不再受到保护！',
      caption: '警告',
      buttons: [
        {
          id: 'shutdown',
          class: 'danger',
          text: '关闭 Portmaster'
        }
      ]
    })
      .onAction('shutdown', () => {
        this.portapi.shutdownPortmaster()
          .subscribe(this.uai.httpObserver(
            '正在关闭……',
            '关闭失败',
          ))
      })
  }
}
