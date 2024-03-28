import { ListKeyManager } from "@angular/cdk/a11y";
import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { CdkPortalOutlet, ComponentPortal } from "@angular/cdk/portal";
import { AfterContentInit, AfterViewInit, ChangeDetectionStrategy, ChangeDetectorRef, Component, ComponentRef, ContentChildren, DestroyRef, ElementRef, EventEmitter, Injector, Input, OnInit, Output, QueryList, ViewChild, ViewChildren, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { ActivatedRoute, Router } from "@angular/router";
import { Observable, Subject } from "rxjs";
import { distinctUntilChanged, map, startWith } from "rxjs/operators";
import { SfngTabComponent, TAB_ANIMATION_DIRECTION, TAB_PORTAL, TAB_SCROLL_HANDLER, TabOutletComponent } from "./tab";

export interface SfngTabContentScrollEvent {
  event?: Event;
  scrollTop: number;
  previousScrollTop: number;
}

/**
 * Tab group component for rendering a tab-style navigation with support for
 * keyboard navigation and type-ahead. Tab content are lazy loaded using a
 * structural directive.
 * The tab group component also supports adding the current active tab index
 * to the active route so it is possible to navigate through tabs using back/forward
 * keys (browser history) as well.
 *
 * Example:
 *  <sfng-tab-group>
 *
 *    <sfng-tab id="tab1" title="Overview">
 *      <div *sfngTabContent>
 *        Some content
 *      </div>
 *    </sfng-tab>
 *
 *    <sfng-tab id="tab2" title="Settings">
 *      <div *sfngTabContent>
 *        Some different content
 *      </div>
 *    </sfng-tab>
 *
 *  </sfng-tab-group>
 */
@Component({
  selector: 'sfng-tab-group',
  templateUrl: './tab-group.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SfngTabGroupComponent implements AfterContentInit, AfterViewInit, OnInit {
  @ContentChildren(SfngTabComponent)
  tabs: QueryList<SfngTabComponent> | null = null;

  /** References to all tab header elements */
  @ViewChildren('tabHeader', { read: ElementRef })
  tabHeaders: QueryList<ElementRef<HTMLDivElement>> | null = null;

  /** Reference to the active tab bar element */
  @ViewChild('activeTabBar', { read: ElementRef, static: false })
  activeTabBar: ElementRef<HTMLDivElement> | null = null;

  /** Reference to the portal outlet that we will use to render a TabOutletComponent. */
  @ViewChild(CdkPortalOutlet, { static: true })
  portalOutlet: CdkPortalOutlet | null = null;

  @Output()
  tabContentScroll = new EventEmitter<SfngTabContentScrollEvent>();

  /** The name of the tab group. Used to update the currently active tab in the route */
  @Input()
  name = 'tab'

  @Input()
  outletClass = '';

  private scrollTop: number = 0;

  /** Whether or not the current tab should be syncronized with the angular router using a query parameter */
  @Input()
  set linkRouter(v: any) {
    this._linkRouter = coerceBooleanProperty(v)
  }
  get linkRouter() { return this._linkRouter }
  private _linkRouter = true;

  /** Whether or not the default tab header should be rendered */
  @Input()
  set customHeader(v: any) {
    this._customHeader = coerceBooleanProperty(v)
  }
  get customHeader() { return this._customHeader }
  private _customHeader = false;

  private tabActivate$ = new Subject<string>();
  private destroyRef = inject(DestroyRef);

  /** Emits the tab QueryList every time there are changes to the content-children */
  get tabs$() {
    return this.tabs?.changes
      .pipe(
        map(() => this.tabs),
        startWith(this.tabs)
      )
  }

  /** onActivate fires when a tab has been activated. */
  get onActivate(): Observable<string> { return this.tabActivate$.asObservable() }

  /** the index of the currently active tab. */
  activeTabIndex = -1;

  /** The key manager used to support keyboard navigation and type-ahead in the tab group */
  private keymanager: ListKeyManager<SfngTabComponent> | null = null;

  /** Used to force the animation direction when calling activateTab. */
  private forceAnimationDirection: 'left' | 'right' | null = null;

  /**
   * pendingTabIdx holds the id or the index of a tab that should be activated after the component
   * has been bootstrapped. We need to cache this value here because the ActivatedRoute might emit
   * before we are AfterViewInit.
   */
  private pendingTabIdx: string | null = null;

  constructor(
    private injector: Injector,
    private route: ActivatedRoute,
    private router: Router,
    private cdr: ChangeDetectorRef
  ) { }

  /**
   * @private
   * Used to forward keyboard events to the keymanager.
   */
  onKeydown(v: KeyboardEvent) {
    this.keymanager?.onKeydown(v);
  }

  ngOnInit(): void {
    this.route.queryParamMap
      .pipe(
        takeUntilDestroyed(this.destroyRef),
        map(params => params.get(this.name)),
        distinctUntilChanged(),
      )
      .subscribe(newIdx => {
        if (!this._linkRouter) {
          return;
        }

        if (!!this.keymanager && !!this.tabs) {
          const actualIndex = this.getIndex(newIdx);
          if (actualIndex !== null) {
            this.keymanager.setActiveItem(actualIndex);
            this.cdr.markForCheck();
          }
        } else {
          this.pendingTabIdx = newIdx;
        }
      })
  }

  ngAfterContentInit(): void {
    this.keymanager = new ListKeyManager(this.tabs!)
      .withHomeAndEnd()
      .withHorizontalOrientation("ltr")
      .withTypeAhead()
      .withWrap()

    this.tabs!.changes
      .subscribe(() => {
        if (this.portalOutlet?.hasAttached()) {
          if (this.tabs!.length === 0) {
            this.portalOutlet.detach();
          }
        } else {
          if (this.tabs!.length > 0) {
            this.activateTab(0)
          }
        }

      })

    this.keymanager.change
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(change => {
        const activeTab = this.tabs!.get(change);
        if (!!activeTab && !!activeTab.tabContent) {
          const prevIdx = this.activeTabIndex;

          let animationDirection: 'left' | 'right' = prevIdx < change ? 'left' : 'right';
          if (this.forceAnimationDirection !== null) {
            animationDirection = this.forceAnimationDirection;
            this.forceAnimationDirection = null;
          }

          if (this.portalOutlet?.attachedRef) {
            // we know for sure that attachedRef is a ComponentRef of TabOutletComponent
            const ref = (this.portalOutlet.attachedRef as ComponentRef<TabOutletComponent>)
            ref.instance._animateDirection = animationDirection;
            ref.instance.outletClass = this.outletClass;
            ref.changeDetectorRef.detectChanges();
          }

          this.portalOutlet?.detach();

          const newOutletPortal = this.createTabOutlet(activeTab, animationDirection);
          this.activeTabIndex = change;
          this.tabContentScroll.next({
            scrollTop: 0,
            previousScrollTop: this.scrollTop,
          })

          this.scrollTop = 0;

          this.tabActivate$.next(activeTab.id);
          this.portalOutlet?.attach(newOutletPortal);

          this.repositionTabBar();

          if (this._linkRouter) {
            this.router.navigate([], {
              queryParams: {
                ...this.route.snapshot.queryParams,
                [this.name]: this.activeTabIndex,
              }
            })
          }
          this.cdr.markForCheck();
        }
      });

    if (this.pendingTabIdx === null) {
      // active the first tab that is NOT disabled
      const firstActivatable = this.tabs?.toArray().findIndex(tap => !tap.disabled);
      if (firstActivatable !== undefined) {
        this.keymanager.setActiveItem(firstActivatable);
      }
    } else {
      const idx = this.getIndex(this.pendingTabIdx);
      if (idx !== null) {
        this.keymanager.setActiveItem(idx);
        this.pendingTabIdx = null;
      }
    }
  }

  ngAfterViewInit(): void {
    this.repositionTabBar();
    this.tabHeaders?.changes.subscribe(() => this.repositionTabBar())
    setTimeout(() => this.repositionTabBar(), 250)
  }

  /**
   * @private
   * Activates a new tab
   *
   * @param idx The index of the new tab.
   */
  activateTab(idx: number, forceDirection?: 'left' | 'right') {
    if (forceDirection !== undefined) {
      this.forceAnimationDirection = forceDirection;
    }

    this.keymanager?.setActiveItem(idx);
  }

  private getIndex(newIdx: string | null): number | null {
    let actualIndex: number = -1;
    if (!this.tabs) {
      return null;
    }

    if (newIdx === undefined || newIdx === null) { // not present in the URL
      return null;
    }
    if (isNaN(+newIdx)) { // likley the ID of a tab
      actualIndex = this.tabs?.toArray().findIndex(tab => tab.id === newIdx) || -1;
    } else { // it's a number as a string
      actualIndex = +newIdx;
    }

    if (actualIndex < 0) {
      return null;
    }
    return actualIndex;
  }

  private repositionTabBar() {
    if (!this.tabHeaders) {
      return;
    }

    requestAnimationFrame(() => {
      const tabHeader = this.tabHeaders!.get(this.activeTabIndex);
      if (!tabHeader || !this.activeTabBar) {
        return;
      }
      const rect = tabHeader.nativeElement.getBoundingClientRect();
      const transform = `translate(${tabHeader.nativeElement.offsetLeft}px, ${tabHeader.nativeElement.offsetTop + rect.height}px)`
      this.activeTabBar.nativeElement.style.width = `${rect.width}px`
      this.activeTabBar.nativeElement.style.transform = transform;
      this.activeTabBar.nativeElement.style.opacity = '1';

      // initialize animations on the active-tab-bar required
      if (!this.activeTabBar.nativeElement.classList.contains("transition-all")) {
        // only initialize the transitions if this is the very first "reposition"
        // this is to prevent the bar from animating to the "bottom" line of the tab
        // header the first time.
        requestAnimationFrame(() => {
          this.activeTabBar?.nativeElement.classList.add("transition-all", "duration-200");
        })
      }
    })
  }

  private createTabOutlet(tab: SfngTabComponent, animationDir: 'left' | 'right'): ComponentPortal<TabOutletComponent> {
    const injector = Injector.create({
      providers: [
        {
          provide: TAB_PORTAL,
          useValue: tab.tabContent!.portal,
        },
        {
          provide: TAB_ANIMATION_DIRECTION,
          useValue: animationDir,
        },
        {
          provide: TAB_SCROLL_HANDLER,
          useValue: (e: Event) => {
            const newScrollTop = (e.target as HTMLElement).scrollTop;

            tab.tabContentScroll.next(e);
            this.tabContentScroll.next({
              event: e,
              scrollTop: newScrollTop,
              previousScrollTop: this.scrollTop,
            });

            this.scrollTop = newScrollTop;
          }
        },
      ],
      parent: this.injector,
      name: 'TabOutletInjectot',
    })

    return new ComponentPortal(
      TabOutletComponent,
      undefined,
      injector
    )
  }
}
