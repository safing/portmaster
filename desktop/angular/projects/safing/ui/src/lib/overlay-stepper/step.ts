import { Injector, TemplateRef, Type } from "@angular/core";
import { Observable } from "rxjs";

export interface Step {
  /**
   * validChange should emit true or false when the current step
   * is valid and the "next" button should be visible.
   */
  validChange: Observable<boolean>;

  /**
   * onBeforeBack, if it exists, is called when the user
   * clicks the "Go Back" button but before the current step
   * is unloaded.
   *
   * The OverlayStepper will wait for the callback to resolve or
   * reject but will not abort going back!
   */
  onBeforeBack?: () => Promise<void>;

  /**
   * onBeforeNext, if it exists, is called when the user
   * clicks the "Next" button but before the current step
   * is unloaded.
   *
   * The OverlayStepper willw ait for the callback to resolve
   * or reject. If it rejects the current step will not be unloaded
   * and the rejected error will be displayed to the user.
   */
  onBeforeNext?: () => Promise<void>;

  /**
   * nextButtonLabel can overwrite the label for the "Next" button.
   */
  nextButtonLabel?: string;

  /**
   * buttonTemplate may hold a tempalte ref that is rendered instead
   * of the default button row with a "Go Back" and a "Next" button.
   * Note that if set, the step component must make sure to handle
   * navigation itself. See {@class StepRef} for more information on how
   * to control the stepper.
   */
  buttonTemplate?: TemplateRef<any>;
}

export interface StepperConfig {
  /**
   * canAbort can be set to a function that is called
   * for each step to determine if the stepper is abortable.
   */
  canAbort?: (idx: number, step: Step) => boolean;

  /** steps holds the list of steps to execute */
  steps: Array<Type<Step>>

  /**
   * injector, if set, defines the parent injector used to
   * create dedicated instances of the step types.
   */
  injector?: Injector;
}


