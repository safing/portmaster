import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { AppIconComponent } from "./app-icon";
import { AppIconResolver, DefaultIconResolver } from "./app-icon-resolver";

@NgModule({
  imports: [
    CommonModule
  ],
  declarations: [
    AppIconComponent,
  ],
  exports: [
    AppIconComponent,
  ],
  providers: [
    {
      provide: AppIconResolver,
      useClass: DefaultIconResolver,
    }
  ]
})
export class SfngAppIconModule { }
