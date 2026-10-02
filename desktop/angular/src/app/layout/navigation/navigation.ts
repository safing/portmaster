import { INTEGRATION_SERVICE, IntegrationService } from 'src/app/integration';
import { ConnectedPosition } from '@angular/cdk/overlay';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, EventEmitter, Inject, OnInit, Output, inject } from '@angular/core';
import { ConfigService, DebugAPI, PortapiService, SPNService, StringSetting, BoolSetting } from '@safing/portmaster-api';
import { tap } from 'rxjs/operators';
import { AppComponent } from 'src/app/app.component';
import { NotificationType, NotificationsService, StatusService, VersionStatus, GetModuleState, ControlPauseStateData } from 'src/app/services';
import { ActionIndicatorService } from 'src/app/shared/action-indicator';
import { fadeInAnimation, fadeOutAnimation } from 'src/app/shared/animations';
import { ExitService } from 'src/app/shared/exit-screen';
import { TauriIntegrationService } from 'src/app/integration/taur-app';

@Component({
  selector: 'app-navigation',
  templateUrl: './navigation.html',
  styleUrls: ['./navigation.scss'],
  exportAs: 'navigation',
  changeDetection: ChangeDetectionStrategy.OnPush,
  animations: [
    fadeInAnimation,
    fadeOutAnimation,
  ]
})
export class NavigationComponent implements OnInit {
  private readonly integration = inject(INTEGRATION_SERVICE);

  /** Emits the current portapi connection state on changes. */
  readonly connected$ = this.portapi.connected$;

  /** @private The available and selected resource versions. */
  versions: VersionStatus | null = null;

  /** Whether or not we have new, unseen notifications */
  hasNewNotifications = false;

  /** The color to use for the notifcation-available hint (dot) */
  notificationColor: string = 'text-green-300';

  pauseState: ControlPauseStateData | null = null;
  get isPaused(): boolean { return this.pauseState?.Interception===true || this.pauseState?.SPN===true; }
  get isPausedInterception(): boolean { return this.pauseState?.Interception===true; }
  get isPausedSPN(): boolean { return this.pauseState?.SPN===true; }
  get pauseInfo(): string {
    if (this.pauseState?.Interception===true && this.pauseState?.SPN===true) 
      return 'Portmaster 和 SPN 已暂停';
    else if (this.pauseState?.Interception===true)
      return 'Portmaster 已暂停';
    else if (this.pauseState?.SPN===true)
      return 'SPN 已暂停';
    return '';
  }
  get pauseInfoTillTime(): string {
    if (this.isPaused && this.pauseState?.TillTime) {
      const date = new Date(this.pauseState.TillTime);
      if (isNaN(date.getTime()) || date.getTime() < Date.now())
        return '';    
      return `将于 ${date.toLocaleTimeString(undefined, { hour12: false })} 自动恢复`;
    }
    return '';
  }

  /** Whether or not we have new, unseen prompts */
  hasNewPrompts = false;

  /** Whether or not prompting is globally enabled. */
  globalPromptingEnabled = false;

  /** Whether or not the SPN is currently enabled */
  spnEnabled = false;

  @Output()
  sideDashChange = new EventEmitter<'collapsed' | 'expanded' | 'force-overlay'>();

  /** Whether or not the side dash should be expanded or collapsed */
  sideDashStatus: 'collapsed' | 'expanded' = 'expanded';

  constructor(
    private portapi: PortapiService,
    private exitService: ExitService,
    private statusService: StatusService,
    private configService: ConfigService,
    private appComponent: AppComponent,
    private debugAPI: DebugAPI,
    private actionIndicator: ActionIndicatorService,
    private notificationService: NotificationsService,
    private spnService: SPNService,
    private cdr: ChangeDetectorRef
  ) { }

  dropDownPositions: ConnectedPosition[] = [
    {
      originX: 'end',
      originY: 'top',
      overlayX: 'start',
      overlayY: 'top'
    }
  ]

  ngOnInit() {
    const mql = window.matchMedia('(max-width: 1200px)');

    if (mql.matches) {
      this.sideDashStatus = 'collapsed';
      this.sideDashChange.next(this.sideDashStatus);
    }

    mql.addEventListener('change', () => {
      if (mql.matches) {
        this.sideDashStatus = 'collapsed';
      } else {
        this.sideDashStatus = 'expanded';
      }
      this.sideDashChange.next(this.sideDashStatus);
    })

    this.statusService.getVersions()
      .subscribe(versions => {
        this.versions = versions;
        this.cdr.markForCheck();
      });

    this.statusService.status$.subscribe(status => {
      this.pauseState = GetModuleState(status, 'Control', 'control:paused')?.Data || null;
    });

    this.configService.watch<StringSetting>('filter/defaultAction')
      .subscribe(defaultAction => {
        this.globalPromptingEnabled = defaultAction === 'ask';
        this.cdr.markForCheck();
      })
    
    this.configService.watch<BoolSetting>("spn/enable")
      .subscribe(value => {
        this.spnEnabled = value;
        this.cdr.markForCheck();
      });

    this.notificationService.new$
      .subscribe(notif => {


        if (notif.some(n => n.Type === NotificationType.Prompt && n.EventID.startsWith("filter:prompt"))) {
          this.hasNewPrompts = true;

          if (this.integration instanceof TauriIntegrationService) {
            this.integration.openPrompt();
          }
        } else {
          this.hasNewPrompts = false;

          if (this.integration instanceof TauriIntegrationService) {
            this.integration.closePrompt();
          }
        }

        if (notif.some(n => !n.EventID.startsWith("filter:prompt"))) {
          this.hasNewNotifications = true;
        } else {
          this.hasNewNotifications = false;
        }

        if (notif.some(n => n.Type === NotificationType.Error)) {
          this.notificationColor = 'text-red-300';
        } else if (notif.some(n => n.Type === NotificationType.Warning)) {
          this.notificationColor = 'text-yellow-300';
        } else {
          this.notificationColor = 'text-green-300';
        }

        this.cdr.markForCheck();
      })
  }

  toggleSideDash(event: MouseEvent) {
    let notify: 'expanded' | 'collapsed' | 'force-overlay' = this.sideDashStatus;

    if (this.sideDashStatus === 'collapsed') {
      this.sideDashStatus = 'expanded';
      notify = 'expanded';
      if (event.shiftKey) {
        notify = 'force-overlay'
      }
    } else {
      this.sideDashStatus = 'collapsed';
      notify = 'collapsed'
    }

    this.sideDashChange.next(notify);
  }

  /**
   * @private
   * Injects a ui/reload event and performs a complete
   * reload of the window once the portmaster re-opened the
   * UI bundle.
   */
  reloadUI(_: Event) {
    this.portapi.reloadUI()
      .pipe(
        tap(() => {
          setTimeout(() => window.location.reload(), 1000)
        })
      )
      .subscribe(this.actionIndicator.httpObserver(
        '正在重新加载界面……',
        '重新加载界面失败',
      ))
  }

  /** Re-initialize the SPN */
  reinitSPN(_: Event) {
    this.portapi.reinitSPN()
      .subscribe(this.actionIndicator.httpObserver(
        '已重新初始化 SPN',
        '重新初始化 SPN 失败'
      ))
  }

  /** Logs the user out of the SPN completely by purgin the user profile from the local storage */
  logoutCompletely(_: Event) {
    this.spnService.logout(true)
      .subscribe(this.actionIndicator.httpObserver(
        '退出登录',
        '您已完全退出 SPN 登录。'
      ))
  }

  /**
   * @private
   * Clear the DNS name cache.
   */
  clearDNSCache(_: Event) {
    this.portapi.clearDNSCache()
      .subscribe(this.actionIndicator.httpObserver(
        'DNS 缓存已清除',
        '清除 DNS 缓存失败。',
      ))
  }

  cleanupHistory(_: Event) {
    this.portapi.cleanupHistory()
      .subscribe(this.actionIndicator.httpObserver(
        '网络历史记录已清理',
        '清理网络历史记录失败。'
      ))
  }

  /**
   * @private
   * Trigger downloading of updates
   *
   * @param event - The mouse event
   */
  downloadUpdates(event: Event) {
    this.portapi.checkForUpdates()
      .subscribe(this.actionIndicator.httpObserver(
        '正在下载更新……',
        '检查更新失败',
      ))
  }

  /**
   * @private
   * Trigger a shutdown of the portmaster-core service
   */
  shutdown(_: Event) {
    this.exitService.shutdownPortmaster();
  }

  /**
   * @private
   * Trigger a restart of the portmaster-core service. Requires
   * that portmaster has been started with a service-wrapper.
   *
   * @param event The mouse event
   */
  restart(event: Event) {
    // prevent default and stop-propagation to avoid
    // expanding the accordion body.
    event.preventDefault();
    event.stopPropagation();

    this.portapi.restartPortmaster()
      .subscribe(this.actionIndicator.httpObserver(
        '正在重启……',
        '重启失败',
      ))
  }

   pause(event: Event, duration: number) {
    // prevent default and stop-propagation to avoid
    // expanding the accordion body.
    event.preventDefault();
    event.stopPropagation();

    this.portapi.pause(duration, false)
      .subscribe(this.actionIndicator.httpObserver(
        '正在暂停……',
        '暂停失败',
      ))
  }
  pauseSPN(event: Event, duration: number) {
    // prevent default and stop-propagation to avoid
    // expanding the accordion body.
    event.preventDefault();
    event.stopPropagation();

    this.portapi.pause(duration, true)
      .subscribe(this.actionIndicator.httpObserver(
        '正在暂停 SPN……',
        '暂停 SPN 失败',
      ))
  }
  resume(event: Event) {
    // prevent default and stop-propagation to avoid
    // expanding the accordion body.
    event.preventDefault();
    event.stopPropagation();

    let msg = '正在恢复……';
    if (this.pauseState?.Interception===true && this.pauseState?.SPN===true) 
      msg = '正在恢复 Portmaster 和 SPN……';
    else if (this.pauseState?.Interception===true)
      msg = '正在恢复 Portmaster……';
    else if (this.pauseState?.SPN===true)
      msg = '正在恢复 SPN……';

    this.portapi.resume()
      .subscribe(this.actionIndicator.httpObserver(
        msg,
        '恢复失败',
      ))
  }
    
  /**
   * @private
   * Opens the data-directory of the portmaster installation.
   * Requires the application to run inside electron.
   */
  async openDataDir(event: Event) {
    const dir = await this.integration.getInstallDir()
    await this.integration.openExternal(dir);
  }

  openChangeLog() {
    const url = "https://github.com/safing/portmaster/releases";
    this.integration.openExternal(url);
  }

  showIntro() {
    this.appComponent.showIntro()
  }

  resetBroadcastState() {
    this.portapi.resetBroadcastState()
      .subscribe(this.actionIndicator.httpObserver(
        '通知状态已清除',
        '重置通知状态失败。',
      ))
  }

  copyDebugInfo(event: Event) {
    // prevent default and stop-propagation to avoid
    // expanding the accordion body.
    event.preventDefault();
    event.stopPropagation();

    this.debugAPI.getCoreDebugInfo()
      .subscribe(
        async info => {
          await this.integration.writeToClipboard(info);
        },
        err => {
          console.error(err);
          this.actionIndicator.error('加载调试数据失败', err);
        }
      )
  }
}
