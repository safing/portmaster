import { animate, style, transition, trigger } from "@angular/animations";
import { ListKeyManagerOption } from "@angular/cdk/a11y";
import { coerceBooleanProperty } from "@angular/cdk/coercion";
import { CdkPortalOutlet, TemplatePortal } from "@angular/cdk/portal";
import { AfterViewInit, ChangeDetectionStrategy, ChangeDetectorRef, Component, ContentChild, Directive, EventEmitter, Inject, InjectionToken, Input, Output, TemplateRef, ViewChild, ViewContainerRef } from "@angular/core";

/** TAB_PORTAL is the injection token used to inject the TabContentDirective portal into TabOutletComponent */
export const TAB_PORTAL = new InjectionToken<TemplatePortal>('TAB_PORTAL');

/** TAB_ANIMATION_DIRECTION is the injection token used to control the :enter animation origin of TabOutletComponent */
export const TAB_ANIMATION_DIRECTION = new InjectionToken<'left' | 'right'>('TAB_ANIMATION_DIRECTION');

/** TAB_SCROLL_HANDLER is called by the SfngTabOutletComponent when a scroll event occurs. */
export const TAB_SCROLL_HANDLER = new InjectionToken<(_: Event) => void>('TAB_SCROLL_HANDLER')

/**
 * Structural directive (*sfngTabContent) to defined lazy-loaded tab content.
 */
@Directive({
  selector: '[sfngTabContent]',
})
export class SfngTabContentDirective<T> {
  portal: TemplatePortal;

  constructor(
    public readonly templateRef: TemplateRef<T>,
    public readonly viewRef: ViewContainerRef,
  ) {
    this.portal = new TemplatePortal(this.templateRef, this.viewRef);
  }
}


/**
 * The tab component that is used to define a new tab as a part of a tab group.
 * The content of the tab is lazy-loaded by using the TabContentDirective.
 */
@Component({
  selector: 'sfng-tab',
  template: '<ng-content></ng-content>',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SfngTabComponent implements ListKeyManagerOption {
  @ContentChild(SfngTabContentDirective, { static: false })
  tabContent: SfngTabContentDirective<any> | null = null;

  /** The ID of the tab used to programatically activate the tab. */
  @Input()
  id = '';

  /** The title for the tab as displayed in the tab group header. */
  @Input()
  title = '';

  /** The key for the tip up in the tab group header. */
  @Input()
  tipUpKey = '';

  @Input()
  set warning(v) {
    this._warning = coerceBooleanProperty(v)
  }
  get warning() { return this._warning }
  private _warning = false;

  /** Emits when the tab content is scrolled */
  @Output()
  tabContentScroll = new EventEmitter<Event>();

  /** Whether or not the tab is currently disabled. */
  @Input()
  set disabled(v: any) {
    this._disabled = coerceBooleanProperty(v);
  }
  get disabled() {
    return this._disabled;
  }
  private _disabled: boolean = false;

  /** getLabel is used by the list key manager to support type-ahead */
  getLabel() { return this.title }
}


/**
 * A simple wrapper component around CdkPortalOutlet to add nice
 * move animations.
 */
@Component({
  selector: 'sfng-tab-outlet',
  template: `
    <div [@moveInOut]="{value: _appAnimate, params: {in: in, out: out}}" class="flex flex-col overflow-auto {{ outletClass }}" (scroll)="onTabContentScroll($event)">
      <ng-template [cdkPortalOutlet]="portal"></ng-template>
    </div>
  `,
  styles: [
    `
    :host{
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    `
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  animations: [
    trigger(
      'moveInOut',
      [
        transition(
          ':enter',
          [
            style({ opacity: 0, transform: 'translateX({{ in }})' }),
            animate('.2s ease-in',
              style({ opacity: 1, transform: 'translateX(0%)' }))
          ],
          { params: { in: '100%' } } // default parameters
        ),
        transition(
          ':leave',
          [
            style({ opacity: 1 }),
            animate('.2s ease-out',
              style({ opacity: 0, transform: 'translateX({{ out }})' }))
          ],
          { params: { out: '-100%' } } // default parameters
        )
      ]
    )]
})
export class TabOutletComponent implements AfterViewInit {
  _appAnimate = false;

  @Input()
  outletClass = ''

  get in() {
    return this._animateDirection == 'left' ? '100%' : '-100%'
  }
  get out() {
    return this._animateDirection == 'left' ? '-100%' : '100%'
  }

  onTabContentScroll(event: Event) {
    if (!!this.scrollHandler) {
      this.scrollHandler(event)
    }
  }

  @ViewChild(CdkPortalOutlet, { static: true })
  portalOutlet!: CdkPortalOutlet;

  constructor(
    @Inject(TAB_PORTAL) public portal: TemplatePortal<any>,
    @Inject(TAB_ANIMATION_DIRECTION) public _animateDirection: 'left' | 'right',
    @Inject(TAB_SCROLL_HANDLER) public scrollHandler: (_: Event) => void,
    private cdr: ChangeDetectorRef
  ) { }

  ngAfterViewInit(): void {
    this.portalOutlet?.attached
      .subscribe(() => {
        this._appAnimate = true;
        this.cdr.detectChanges();
      })
  }
}
