import { CommonModule } from '@angular/common';
import { NgModule } from '@angular/core';
import { LocalizedValuePipe } from './localized-value.pipe';
import { TranslatePipe } from './translate.pipe';

@NgModule({
  imports: [CommonModule],
  declarations: [TranslatePipe, LocalizedValuePipe],
  exports: [TranslatePipe, LocalizedValuePipe],
})
export class I18nModule { }
