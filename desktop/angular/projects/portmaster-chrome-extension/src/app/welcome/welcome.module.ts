import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { OverlayStepperModule } from "@safing/ui";
import { IntroComponent } from "./intro.component";

@NgModule({
  imports: [
    CommonModule,
    OverlayStepperModule,
  ],
  declarations: [
    IntroComponent,
  ],
  exports: [
    IntroComponent,
  ]
})
export class WelcomeModule { }

