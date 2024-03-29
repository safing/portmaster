import { OverlayModule } from "@angular/cdk/overlay";
import { PortalModule } from "@angular/cdk/portal";
import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { SfngDialogModule } from "../dialog";
import { OverlayStepperContainerComponent } from "./overlay-stepper-container";
import { StepOutletComponent } from "./step-outlet";

@NgModule({
  imports: [
    CommonModule,
    PortalModule,
    OverlayModule,
    SfngDialogModule,
  ],
  declarations: [
    OverlayStepperContainerComponent,
    StepOutletComponent,
  ]
})
export class OverlayStepperModule { }
