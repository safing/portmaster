import { InjectionToken } from "@angular/core";
import { Observable } from "rxjs";
import { take } from "rxjs/operators";
import { OverlayStepperContainerComponent } from "./overlay-stepper-container";

/**
 * STEP_REF is the injection token that is used to provide a reference to the
 * Stepper to each step.
 */
export const STEP_REF = new InjectionToken<StepRef<any>>('StepRef')

export interface StepperControl {
  /**
   * Next should move the stepper forward to the next
   * step or close the stepper if no more steps are
   * available.
   * If the stepper is closed this way all onFinish hooks
   * registered at {@link StepRef} are executed.
   */
  next(): Promise<void>;

  /**
   * goBack should move the stepper back to the previous
   * step. This is a no-op if there's no previous step to
   * display.
   */
  goBack(): Promise<void>;

  /**
   * close closes the stepper but does not run any onFinish hooks
   * of {@link StepRef}.
   */
  close(): Promise<void>;
}

/**
 * StepRef is a reference to the overlay stepper and can be used to control, abort
 * or otherwise interact with the stepper.
 *
 * It is made available to individual steps using the STEP_REF injection token.
 * Each step in the OverlayStepper receives it's own StepRef instance and will receive
 * a reference to the same instance in case the user goes back and re-opens a step
 * again.
 *
 * Steps should therefore store any configuration data that is needed to restore
 * the previous view in the StepRef using it's save() and load() methods.
 */
export class StepRef<T = any> implements StepperControl {
  private onFinishHooks: (() => PromiseLike<void> | void)[] = [];
  private data: T | null = null;

  constructor(
    private currentStepIndex: number,
    private stepContainerRef: OverlayStepperContainerComponent,
  ) {
    this.stepContainerRef.onFinish
      .pipe(take(1))
      .subscribe(() => this.runOnFinishHooks)
  }

  next(): Promise<void> {
    return this.stepContainerRef.next();
  }

  goBack(): Promise<void> {
    return this.stepContainerRef.goBack();
  }

  close(): Promise<void> {
    return this.stepContainerRef.close();
  }

  /**
   * Save saves data of the current step in the stepper session.
   * This data is saved in case the user decides to "go back" to
   * to a previous step so the old view can be restored.
   *
   * @param data The data to save in the stepper session.
   */
  save(data: T): void {
    this.data = data;
  }

  /**
   * Load returns the data previously stored using save(). The
   * StepperRef automatically makes sure the correct data is returned
   * for the current step.
   */
  load(): T | null {
    return this.data;
  }

  /**
   * registerOnFinish registers fn to be called when the last step
   * completes and the stepper is going to finish.
   */
  registerOnFinish(fn: () => PromiseLike<void> | void) {
    this.onFinishHooks.push(fn);
  }

  /**
   * Executes all onFinishHooks in the order they have been defined
   * and waits for each hook to complete.
   */
  private async runOnFinishHooks() {
    for (let i = 0; i < this.onFinishHooks.length; i++) {
      let res = this.onFinishHooks[i]();
      if (typeof res === 'object' && 'then' in res) {
        // res is a PromiseLike so wait for it
        try {
          await res;
        } catch (err) {
          console.error(`Failed to execute on-finish hook of step ${this.currentStepIndex}: `, err)
        }
      }
    }
  }
}


export class StepperRef implements StepperControl {
  constructor(private stepContainerRef: OverlayStepperContainerComponent) { }

  next(): Promise<void> {
    return this.stepContainerRef.next();
  }

  goBack(): Promise<void> {
    return this.stepContainerRef.goBack();
  }

  close(): Promise<void> {
    return this.stepContainerRef.close();
  }

  get onFinish(): Observable<void> {
    return this.stepContainerRef.onFinish;
  }

  get onClose(): Observable<void> {
    return this.stepContainerRef.onClose;
  }
}
