import { Overlay } from '@angular/cdk/overlay';
import { AfterViewInit, ChangeDetectorRef, Component, ElementRef, HostListener, Inject, NgZone, OnInit, Renderer2, ViewChild } from '@angular/core';
import { Params, Router } from '@angular/router';
import { PortapiService } from '@safing/portmaster-api';
import { OverlayStepper, SfngDialogService, StepperRef } from '@safing/ui';
import { BehaviorSubject, merge, Subject } from 'rxjs';
import { debounceTime, filter, mergeMap, skip, startWith, take } from 'rxjs/operators';
import { IntroModule } from './intro';
import { NotificationsService, UIStateService } from './services';
import { ActionIndicatorService } from './shared/action-indicator';
import { fadeInAnimation, fadeOutAnimation } from './shared/animations';
import { ExitService } from './shared/exit-screen';
import { SfngNetquerySearchOverlayComponent } from './shared/netquery/search-overlay';
import { INTEGRATION_SERVICE, IntegrationService } from './integration';
import { TauriIntegrationService } from './integration/taur-app';

@Component({
  selector: 'app-root',
  templateUrl: './app.component.html',
  styleUrls: ['./app.component.scss'],
  animations: [
    fadeInAnimation,
    fadeOutAnimation,
  ]
})
export class AppComponent implements OnInit, AfterViewInit {
  readonly connected = this.portapi.connected$.pipe(
    debounceTime(250),
    startWith(false)
  );
  title = 'portmaster';

  /** The current status of the side dash as emitted by the navigation component */
  sideDashStatus: 'collapsed' | 'expanded' = 'expanded';

  /** Whether or not the side-dash is in overlay mode */
  sideDashOverlay = false;

  /** The MQL to watch for screen size changes. */
  private mql!: MediaQueryList;

  /** Emits when the side-dash is opened or closed in non-overlay mode */
  private sideDashOpen = new BehaviorSubject<boolean>(false);

  /** Used to emit when the window size changed */
  windowResizeChange = new Subject<void>();

  get sideDashOpen$() { return this.sideDashOpen.asObservable() }

  get showOverlay$() { return this.exitService.showOverlay$ }

  get onContentSizeChange$() {
    return merge(
      this.windowResizeChange,
      this.sideDashOpen$
    )
      .pipe(
        startWith(undefined),
        debounceTime(100),
      )
  }

  @ViewChild('mainContent', { read: ElementRef, static: true })
  mainContent!: ElementRef<HTMLDivElement>;

  @HostListener('window:resize')
  onWindowResize() {
    this.windowResizeChange.next();
  }

  @HostListener('document:keydown', ['$event'])
  onKeyDown(event: KeyboardEvent) {
    if (event.key === ' ' && event.ctrlKey) {
      this.dialog.create(
        SfngNetquerySearchOverlayComponent,
        {
          positionStrategy: this.overlay
            .position()
            .global()
            .centerHorizontally()
            .top('1rem'),
          backdrop: 'light',
          autoclose: true,
        }
      )
      return;
    }
  }

  constructor(
    public ngZone: NgZone,
    public portapi: PortapiService,
    public changeDetectorRef: ChangeDetectorRef,
    private router: Router,
    private exitService: ExitService,
    private overlayStepper: OverlayStepper,
    private dialog: SfngDialogService,
    private overlay: Overlay,
    private stateService: UIStateService,
    private renderer2: Renderer2,
    @Inject(INTEGRATION_SERVICE) private integration: IntegrationService,
  ) {
    (window as any).portapi = portapi;
  }

  onSideDashChange(state: 'expanded' | 'collapsed' | 'force-overlay') {
    if (state === 'force-overlay') {
      state = 'expanded';
      if (!this.sideDashOverlay) {
        this.sideDashOverlay = true;
      }
    } else {
      this.sideDashOverlay = this.mql.matches;
    }

    this.sideDashStatus = state;

    if (!this.sideDashOverlay) {
      this.sideDashOpen.next(this.sideDashStatus === 'expanded')
    }
  }

  ngOnInit() {
    // default breakpoints used by tailwindcss
    const minContentWithBp = [
      640,  // sfng-sm:
      768,  // sfng-md:
      1024, // sfng-lg:
      1280, // sfng-xl:
      1536  // sfng-2xl:
    ]

    // prepare our breakpoint listeners and add the classes to our main element
    merge(
      this.windowResizeChange,
      this.sideDashOpen$
    )
      .pipe(
        startWith(undefined),
        debounceTime(100),
      )
      .subscribe(() => {
        const rect = (this.mainContent.nativeElement as HTMLElement).getBoundingClientRect();

        minContentWithBp.forEach((bp, idx) => {
          if (rect.width >= bp) {
            this.renderer2.addClass(this.mainContent.nativeElement, `min-width-${bp}px`)
          } else {
            this.renderer2.removeClass(this.mainContent.nativeElement, `min-width-${bp}px`)
          }
        })

        this.changeDetectorRef.markForCheck();
      })

    // force a reload of the current route if we reconnected to
    // portmaster. This ensures we'll refresh any data that's currently
    // displayed.
    this.connected
      .pipe(
        filter(connected => !!connected),
        skip(1),
      )
      .subscribe(async () => {
        const location = new URL(window.location.toString());

        const params: Params = {}
        location.searchParams.forEach((value, key) => {
          params[key] = [
            ...(params[key] || []),
            value,
          ]
        })

        await this.router.navigateByUrl('/', { skipLocationChange: true })
        this.router.navigate([location.pathname], {
          queryParams: params,
        });
      })

    this.stateService.uiState()
      .pipe(take(1))
      .subscribe(state => {
        if (!state.introScreenFinished) {
          this.showIntro();
        }
      })

    this.mql = window.matchMedia('(max-width: 1200px)');
    this.sideDashOverlay = this.mql.matches;

    this.mql.addEventListener('change', () => {
      this.sideDashOverlay = this.mql.matches;

      if (!this.sideDashOverlay) {
        this.sideDashOpen.next(this.sideDashStatus === 'expanded')
      }
    })
  }

  ngAfterViewInit(): void {
    this.sideDashOpen.next(this.sideDashStatus !== 'collapsed')

    if (this.integration instanceof TauriIntegrationService) {
      let tauri = this.integration;

      tauri.shouldShow()
        .then(show => {
          console.log("should open window: ", show)
          if (show) {
            tauri.openApp();
          }
        });
    }
  }

  showIntro(): StepperRef {
    const stepperRef = this.overlayStepper.create(IntroModule.Stepper)

    stepperRef.onFinish.subscribe(() => {
      this.stateService.uiState()
        .pipe(
          take(1),
          mergeMap(state => this.stateService.saveState({
            ...state,
            introScreenFinished: true
          }))
        )
        .subscribe();
    })

    return stepperRef;
  }
}
