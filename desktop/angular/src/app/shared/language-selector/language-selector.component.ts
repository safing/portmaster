import { Component, OnDestroy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { Subject, takeUntil } from 'rxjs';
import { LocaleService, AVAILABLE_LANGUAGES } from '../../services';

@Component({
  selector: 'app-language-selector',
  standalone: true,
  imports: [CommonModule],
  template: `
    <div class="language-selector">
      <select 
        [value]="currentLang" 
        (change)="onLanguageChange($event)"
        class="px-2 py-1 text-xs bg-gray-300 border-none rounded-md cursor-pointer text-secondary hover:bg-gray-400 focus:outline-none focus:ring-1 focus:ring-blue"
      >
        <option *ngFor="let lang of languages" [value]="lang.code">
          {{ lang.nativeName }}
        </option>
      </select>
    </div>
  `,
  styles: [`
    .language-selector select {
      appearance: none;
      background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' fill='%239ca3af' viewBox='0 0 16 16'%3E%3Cpath d='M7.247 11.14 2.451 5.658C1.885 5.013 2.345 4 3.204 4h9.592a1 1 0 0 1 .753 1.659l-4.796 5.48a1 1 0 0 1-1.506 0z'/%3E%3C/svg%3E");
      background-repeat: no-repeat;
      background-position: right 0.5rem center;
      padding-right: 1.75rem;
    }
  `]
})
export class LanguageSelectorComponent implements OnDestroy {
  languages = AVAILABLE_LANGUAGES;
  currentLang: string;
  private readonly destroy$ = new Subject<void>();

  constructor(private readonly localeService: LocaleService) {
    this.currentLang = this.localeService.getCurrentLanguage();
    
    this.localeService.currentLanguage$
      .pipe(takeUntil(this.destroy$))
      .subscribe(lang => {
        this.currentLang = lang;
      });
  }

  ngOnDestroy(): void {
    this.destroy$.next();
    this.destroy$.complete();
  }

  onLanguageChange(event: Event): void {
    const select = event.target as HTMLSelectElement;
    this.localeService.setLanguage(select.value);
  }
}
