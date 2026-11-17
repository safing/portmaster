import { CommonModule } from '@angular/common';
import { NgModule } from '@angular/core';
import { I18nVersionPipe } from './i18n-version.pipe';
import { LocalizedValuePipe } from './localized-value.pipe';
import { TranslatePipe } from './translate.pipe';

const PIPES = [TranslatePipe, LocalizedValuePipe, I18nVersionPipe];

@NgModule({
  imports: [CommonModule],
  declarations: PIPES,
  exports: PIPES,
})
export class I18nModule { }
