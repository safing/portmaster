import { DragDropModule } from "@angular/cdk/drag-drop";
import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { SfngTipUpModule, SfngTooltipModule } from "@safing/ui";
import { MultiSwitchComponent } from "./multi-switch";
import { SwitchItemComponent } from "./switch-item";

@NgModule({
  imports: [
    CommonModule,
    FormsModule,
    SfngTooltipModule,
    SfngTipUpModule,
    DragDropModule,
  ],
  declarations: [
    MultiSwitchComponent,
    SwitchItemComponent,
  ],
  exports: [
    MultiSwitchComponent,
    SwitchItemComponent,
  ],
})
export class SfngMultiSwitchModule { }
