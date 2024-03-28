import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { SfngPaginationContentDirective } from ".";
import { SfngPaginationWrapperComponent } from "./pagination";

@NgModule({
  imports: [
    CommonModule,
  ],
  declarations: [
    SfngPaginationContentDirective,
    SfngPaginationWrapperComponent,
  ],
  exports: [
    SfngPaginationContentDirective,
    SfngPaginationWrapperComponent,
  ],
})
export class SfngPaginationModule { }
