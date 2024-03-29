import { ComponentRef, Injectable, Injector } from "@angular/core";
import { SfngDialogService } from "../dialog";
import { OverlayStepperContainerComponent, STEP_CONFIG } from "./overlay-stepper-container";
import { OverlayStepperModule } from "./overlay-stepper.module";
import { StepperRef } from "./refs";
import { StepperConfig } from "./step";

@Injectable({ providedIn: OverlayStepperModule })
export class OverlayStepper {
  constructor(
    private injector: Injector,
    private dialog: SfngDialogService,
  ) { }

  /**
   * Creates a new overlay stepper given it's configuration and returns
   * a reference to the stepper that can be used to wait for or control
   * the stepper from outside.
   *
   * @param config The configuration for the overlay stepper.
   */
  create(config: StepperConfig): StepperRef {
    // create a new injector for our OverlayStepperContainer
    // that holds a reference to the StepperConfig.
    const injector = this.createInjector(config);

    const dialogRef = this.dialog.create(OverlayStepperContainerComponent, {
      injector: injector,
      autoclose: false,
      backdrop: 'light',
      dragable: false,
    })

    const containerComponentRef = dialogRef.contentRef() as ComponentRef<OverlayStepperContainerComponent>;

    return new StepperRef(containerComponentRef.instance);
  }

  /**
   * Creates a new dependency injector that provides access to the
   * stepper configuration using the STEP_CONFIG injection token.
   *
   * @param config The stepper configuration to provide using DI
   * @returns
   */
  private createInjector(config: StepperConfig): Injector {
    return Injector.create({
      providers: [
        {
          provide: STEP_CONFIG,
          useValue: config,
        },
      ],
      parent: this.injector,
    })
  }
}
