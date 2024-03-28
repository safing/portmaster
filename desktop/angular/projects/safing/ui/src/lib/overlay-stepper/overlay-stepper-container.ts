import { animate, style, transition, trigger } from "@angular/animations";
import { CdkPortalOutlet, ComponentPortal, ComponentType } from "@angular/cdk/portal";
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, ComponentRef, Inject, InjectionToken, Injector, isDevMode, OnDestroy, OnInit, ViewChild } from "@angular/core";
import { Subject } from "rxjs";
import { SfngDialogRef, SFNG_DIALOG_REF } from "../dialog";
import { StepperControl, StepRef, STEP_REF } from "./refs";
import { Step, StepperConfig } from "./step";
import { StepOutletComponent, STEP_ANIMATION_DIRECTION, STEP_PORTAL } from "./step-outlet";

/**
 * STEP_CONFIG is used to inject the StepperConfig into the OverlayStepperContainer.
 */
export const STEP_CONFIG = new InjectionToken<StepperConfig>('StepperConfig');

@Component({
  templateUrl: './overlay-stepper-container.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [
    `
    :host {
      position: relative;
      display: flex;
      flex-direction: column;
      width: 600px;
    }
    `
  ],
  animations: [
    trigger(
      'moveInOut',
      [
        transition(
          ':enter',
          [
            style({ opacity: 0, transform: 'translateX({{ in }})' }),
            animate('.2s cubic-bezier(0.4, 0, 0.2, 1)',
              style({ opacity: 1, transform: 'translateX(0%)' }))
          ],
          { params: { in: '100%' } } // default parameters
        ),
        transition(
          ':leave',
          [
            style({ opacity: 1 }),
            animate('.2s cubic-bezier(0.4, 0, 0.2, 1)',
              style({ opacity: 0, transform: 'translateX({{ out }})' }))
          ],
          { params: { out: '-100%' } } // default parameters
        )
      ]
    )]
})
export class OverlayStepperContainerComponent implements OnInit, OnDestroy, StepperControl {
  /** Used to keep cache the stepRef instances. See documentation for {@class StepRef} */
  private stepRefCache = new Map<number, StepRef>();

  /** Used to emit when the stepper finished. This is always folled by emitting on onClose$ */
  private onFinish$ = new Subject<void>();

  /** Emits when the stepper finished - also see {@link OverlayStepperContainerComponent.onClose}*/
  get onFinish() {
    return this.onFinish$.asObservable();
  }

  /**
   * Emits when the stepper is closed.
   * If the stepper if finished then onFinish will emit first
   */
  get onClose() {
    return this.dialogRef.onClose;
  }

  /** The index of the currently displayed step */
  currentStepIndex = -1;

  /** The component instance of the current step */
  currentStep: Step | null = null;

  /** A reference to the portalOutlet used to render our steps */
  @ViewChild(CdkPortalOutlet, { static: true })
  portalOutlet!: CdkPortalOutlet;

  /** Whether or not the user can go back */
  canGoBack = false;

  /** Whether or not the user can abort and close the stepper */
  canAbort = false;

  /** Whether the current step is the last step */
  get isLast() {
    return this.currentStepIndex + 1 >= this.config.steps.length;
  }

  constructor(
    @Inject(STEP_CONFIG) public readonly config: StepperConfig,
    @Inject(SFNG_DIALOG_REF) public readonly dialogRef: SfngDialogRef<void>,
    private injector: Injector,
    private cdr: ChangeDetectorRef
  ) { }

  /**
   * Moves forward to the next step or closes the stepper
   * when moving beyond the last one.
   */
  next(): Promise<void> {
    if (this.isLast) {
      this.onFinish$.next();
      this.close();

      return Promise.resolve();
    }

    return this.attachStep(this.currentStepIndex + 1, true)
  }

  /**
   * Moves back to the previous step. This does not take canGoBack
   * into account.
   */
  goBack(): Promise<void> {
    return this.attachStep(this.currentStepIndex - 1, false)
  }


  /** Closes the stepper - this does not run the onFinish hooks of the steps */
  async close(): Promise<void> {
    this.dialogRef.close();
  }

  ngOnInit(): void {
    this.next();
  }

  ngOnDestroy(): void {
    this.onFinish$.complete();
  }

  /**
   * Attaches a new step component in the current outlet. It detaches any previous
   * step and calls onBeforeBack and onBeforeNext respectively.
   *
   * @param index The index of the new step to attach.
   * @param forward Whether or not the new step is attached by going "forward" or "backward"
   * @returns
   */
  private async attachStep(index: number, forward = true) {
    if (index >= this.config.steps.length) {
      if (isDevMode()) {
        throw new Error(`Cannot attach step at ${index}: index out of range`)
      }
      return;
    }

    // call onBeforeNext or onBeforeBack of the current step
    if (this.currentStep) {
      if (forward) {
        if (!!this.currentStep.onBeforeNext) {
          try {
            await this.currentStep.onBeforeNext();
          } catch (err) {
            console.error(`Failed to move to next step`, err)
            // TODO(ppacher): display error

            return;
          }
        }
      } else {
        if (!!this.currentStep.onBeforeBack) {
          try {
            await this.currentStep.onBeforeBack()
          } catch (err) {
            console.error(`Step onBeforeBack callback failed`, err)
          }
        }
      }

      // detach the current step component.
      this.portalOutlet.detach();
    }

    const stepType = this.config.steps[index];
    const contentPortal = this.createStepContentPortal(stepType, index)
    const outletPortal = this.createStepOutletPortal(contentPortal, forward ? 'right' : 'left')

    // attach the new step (which is wrapped in a StepOutletComponent).
    const ref = this.portalOutlet.attachComponentPortal(outletPortal);

    // We need to wait for the step to be actually attached in the outlet
    // to get access to the actual step component instance.
    ref.instance.portalOutlet!.attached
      .subscribe((stepRef: ComponentRef<Step>) => {
        this.currentStep = stepRef.instance;
        this.currentStepIndex = index;

        if (typeof this.config.canAbort === 'function') {
          this.canAbort = this.config.canAbort(this.currentStepIndex, this.currentStep);
        }

        // make sure we trigger a change-detection cycle now
        // markForCheck() is not enough here as we need a CD to run
        // immediately for the Step.buttonTemplate to be accounted for correctly.
        this.cdr.detectChanges();
      })
  }

  /**
   * Creates a new component portal for a step and provides access to the {@class StepRef}
   * using dependency injection.
   *
   * @param stepType The component type of the step for which a new portal should be created.
   * @param index The index of the current step. Used to create/cache the {@class StepRef}
   */
  private createStepContentPortal(stepType: ComponentType<Step>, index: number): ComponentPortal<Step> {
    let stepRef = this.stepRefCache.get(index);
    if (stepRef === undefined) {
      stepRef = new StepRef(index, this)
      this.stepRefCache.set(index, stepRef);
    }

    const injector = Injector.create({
      providers: [
        {
          provide: STEP_REF,
          useValue: stepRef,
        }
      ],
      parent: this.config.injector || this.injector,
    })

    return new ComponentPortal(stepType, undefined, injector);
  }

  /**
   * Creates a new component portal for a step outlet component that will attach another content
   * portal and wrap the attachment in a "move in" animation for a given direction.
   *
   * @param contentPortal The portal of the actual content that should be attached in the outlet
   * @param dir The direction for the animation of the step outlet.
   */
  private createStepOutletPortal(contentPortal: ComponentPortal<Step>, dir: 'left' | 'right'): ComponentPortal<StepOutletComponent> {
    const injector = Injector.create({
      providers: [
        {
          provide: STEP_PORTAL,
          useValue: contentPortal,
        },
        {
          provide: STEP_ANIMATION_DIRECTION,
          useValue: dir,
        },
      ],
      parent: this.injector,
    })

    return new ComponentPortal(
      StepOutletComponent,
      undefined,
      injector,
    )
  }
}
