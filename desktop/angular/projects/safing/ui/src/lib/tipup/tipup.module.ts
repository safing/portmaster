import { CommonModule } from "@angular/common";
import { ModuleWithProviders, NgModule, Type } from "@angular/core";
import { MarkdownModule } from "ngx-markdown";
import { SfngDialogModule } from "../dialog";
import { SfngTipUpAnchorDirective } from './anchor';
import { SfngsfngTipUpTriggerDirective, SfngTipUpIconComponent } from './tipup';
import { SfngTipUpComponent } from './tipup-component';
import { ActionRunner, HelpTexts, SFNG_TIP_UP_ACTION_RUNNER, SFNG_TIP_UP_CONTENTS } from "./translations";
import { SafePipe } from "./safe.pipe";

@NgModule({
  imports: [
    CommonModule,
    MarkdownModule.forChild(),
    SfngDialogModule,
  ],
  declarations: [
    SfngTipUpIconComponent,
    SfngsfngTipUpTriggerDirective,
    SfngTipUpComponent,
    SfngTipUpAnchorDirective,
    SafePipe
  ],
  exports: [
    SfngTipUpIconComponent,
    SfngsfngTipUpTriggerDirective,
    SfngTipUpComponent,
    SfngTipUpAnchorDirective
  ],
})
export class SfngTipUpModule {
  static forRoot(text: HelpTexts<any>, runner: Type<ActionRunner<any>>): ModuleWithProviders<SfngTipUpModule> {
    return {
      ngModule: SfngTipUpModule,
      providers: [
        {
          provide: SFNG_TIP_UP_CONTENTS,
          useValue: text,
        },
        {
          provide: SFNG_TIP_UP_ACTION_RUNNER,
          useExisting: runner,
        }
      ]
    }
  }
}
