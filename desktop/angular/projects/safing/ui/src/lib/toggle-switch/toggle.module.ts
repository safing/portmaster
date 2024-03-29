import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { SfngToggleSwitchComponent } from "./toggle-switch";

@NgModule({
  imports: [
    CommonModule,
    FormsModule,
  ],
  declarations: [
    SfngToggleSwitchComponent,
  ],
  exports: [
    SfngToggleSwitchComponent,
  ]
})
export class SfngToggleSwitchModule { }
