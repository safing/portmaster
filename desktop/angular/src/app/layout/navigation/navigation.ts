import { INTEGRATION_SERVICE, IntegrationService } from 'src/app/integration';
import { ConnectedPosition } from '@angular/cdk/overlay';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, EventEmitter, Inject, OnInit, Output, inject } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ConfigService, DebugAPI, PortapiService, SPNService, StringSetting, BoolSetting } from '@safing/portmaster-api';
import { tap } from 'rxjs/operators';
import { I18nService } from 'src/i18n';
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
  private readonly destroyRef = inject(DestroyRef);

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
      return this.i18n.t('pause.both');
    else if (this.pauseState?.Interception===true)
      return this.i18n.t('pause.pm');
    else if (this.pauseState?.SPN===true)
      return this.i18n.t('pause.spn');
    return '';
  }
  get pauseInfoTillTime(): string {
    if (this.isPaused && this.pauseState?.TillTime) {
      const date = new Date(this.pauseState.TillTime);
      if (isNaN(date.getTime()) || date.getTime() < Date.now())
        return '';    
      return this.i18n.t('pause.autoResume', { time: date.toLocaleTimeString(undefined, { hour12: false }) });
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
    private cdr: ChangeDetectorRef,
    private i18n: I18nService,
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
    // OnPush view: refresh translated strings when the UI language changes.
    this.i18n.language$
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(() => this.cdr.markForCheck());

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
        this.i18n.t('nav.reloadingUI'),
        this.i18n.t('nav.reloadUIFailed'),
      ))
  }

  /** Re-initialize the SPN */
  reinitSPN(_: Event) {
    this.portapi.reinitSPN()
      .subscribe(this.actionIndicator.httpObserver(
        this.i18n.t('nav.spnReinitialized'),
        this.i18n.t('nav.spnReinitFailed')
      ))
  }

  /** Logs the user out of the SPN completely by purgin the user profile from the local storage */
  logoutCompletely(_: Event) {
    this.spnService.logout(true)
      .subscribe(this.actionIndicator.httpObserver(
        this.i18n.t('dash.logout'),
        this.i18n.t('dash.logoutDone')
      ))
  }

  /**
   * @private
   * Clear the DNS name cache.
   */
  clearDNSCache(_: Event) {
    this.portapi.clearDNSCache()
      .subscribe(this.actionIndicator.httpObserver(
        this.i18n.t('nav.dnsCacheCleared'),
        this.i18n.t('nav.dnsCacheClearFailed'),
      ))
  }

  cleanupHistory(_: Event) {
    this.portapi.cleanupHistory()
      .subscribe(this.actionIndicator.httpObserver(
        this.i18n.t('nav.historyCleaned'),
        this.i18n.t('nav.historyCleanFailed')
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
        this.i18n.t('nav.updatesDownloading'),
        this.i18n.t('nav.updatesCheckFailed'),
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
        this.i18n.t('nav.restarting'),
        this.i18n.t('nav.restartFailed'),
      ))
  }

   pause(event: Event, duration: number) {
    // prevent default and stop-propagation to avoid
    // expanding the accordion body.
    event.preventDefault();
    event.stopPropagation();

    this.portapi.pause(duration, false)
      .subscribe(this.actionIndicator.httpObserver(
        this.i18n.t('nav.pausing'),
        this.i18n.t('nav.pauseFailed'),
      ))
  }
  pauseSPN(event: Event, duration: number) {
    // prevent default and stop-propagation to avoid
    // expanding the accordion body.
    event.preventDefault();
    event.stopPropagation();

    this.portapi.pause(duration, true)
      .subscribe(this.actionIndicator.httpObserver(
        this.i18n.t('nav.pausingSPN'),
        this.i18n.t('nav.pauseSPNFailed'),
      ))
  }
  resume(event: Event) {
    // prevent default and stop-propagation to avoid
    // expanding the accordion body.
    event.preventDefault();
    event.stopPropagation();

    let msg = this.i18n.t('nav.resuming');
    if (this.pauseState?.Interception===true && this.pauseState?.SPN===true) 
      msg = this.i18n.t('nav.resumingBoth');
    else if (this.pauseState?.Interception===true)
      msg = this.i18n.t('nav.resumingPM');
    else if (this.pauseState?.SPN===true)
      msg = this.i18n.t('nav.resumingSPN');

    this.portapi.resume()
      .subscribe(this.actionIndicator.httpObserver(
        msg,
        this.i18n.t('nav.resumeFailed'),
      ))
  }
    
  /**
   * @private
   * Opens the data directory of the portmaster installation in the
   * system file manager. Not supported when running in a browser.
   */
  openDataDir(event: Event) {
    this.integration.openDir('data')
      .catch(err => this.actionIndicator.error(this.i18n.t('nav.dataDirFailed'), err));
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
        this.i18n.t('nav.notificationsCleared'),
        this.i18n.t('nav.notificationsClearFailed'),
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
          this.actionIndicator.error(this.i18n.t('nav.debugLoadFailed'), err);
        }
      )
  }
}
