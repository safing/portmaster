import { animate, style, transition, trigger } from "@angular/animations";
import { CdkPortalOutlet, ComponentPortal } from "@angular/cdk/portal";
import { AfterViewInit, ChangeDetectionStrategy, ChangeDetectorRef, Component, ComponentRef, Inject, InjectionToken, ViewChild } from "@angular/core";
import { Step } from "./step";

export const STEP_PORTAL = new InjectionToken<ComponentPortal<Step>>('STEP_PORTAL')
export const STEP_ANIMATION_DIRECTION = new InjectionToken<'left' | 'right'>('STEP_ANIMATION_DIRECTION');

/**
 * A simple wrapper component around CdkPortalOutlet to add nice
 * move animations.
 */
@Component({
  template: `
    <div [@moveInOut]="{value: _appAnimate, params: {in: in, out: out}}" class="flex flex-col overflow-auto">
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
export class StepOutletComponent implements AfterViewInit {
  /** @private - Whether or not the animation should run. */
  _appAnimate = false;

  /** The actual step instance that has been attached. */
  stepInstance: ComponentRef<Step> | null = null;

  /** @private - used in animation interpolation for translateX  */
  get in() {
    return this._animateDirection == 'left' ? '-100%' : '100%'
  }

  /** @private - used in animation interpolation for traslateX  */
  get out() {
    return this._animateDirection == 'left' ? '100%' : '-100%'
  }

  /** The portal outlet in our view used to attach the step */
  @ViewChild(CdkPortalOutlet, { static: true })
  portalOutlet!: CdkPortalOutlet;

  constructor(
    @Inject(STEP_PORTAL) public portal: ComponentPortal<Step>,
    @Inject(STEP_ANIMATION_DIRECTION) public _animateDirection: 'left' | 'right',
    private cdr: ChangeDetectorRef
  ) { }

  ngAfterViewInit(): void {
    this.portalOutlet?.attached
      .subscribe(ref => {
        this.stepInstance = ref as ComponentRef<Step>;

        this._appAnimate = true;
        this.cdr.detectChanges();
      })
  }
}
