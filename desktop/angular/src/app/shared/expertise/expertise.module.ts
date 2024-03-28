import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { SfngSelectModule, SfngTipUpModule } from "@safing/ui";
import { ExpertiseDirective } from "./expertise-directive";
import { ExpertiseComponent } from "./expertise-switch";

@NgModule({
  imports: [
    SfngSelectModule,
    CommonModule,
    SfngTipUpModule,
    FormsModule,
  ],
  declarations: [
    ExpertiseComponent,
    ExpertiseDirective,
  ],
  exports: [
    ExpertiseComponent,
    ExpertiseDirective,
  ]
})
export class ExpertiseModule { }
