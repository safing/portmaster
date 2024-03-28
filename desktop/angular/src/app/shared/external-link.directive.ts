import { isPlatformBrowser } from '@angular/common';
import {
  Directive,
  HostBinding, HostListener, Inject,
  Input, OnChanges, PLATFORM_ID, inject
} from '@angular/core';
import { INTEGRATION_SERVICE } from '../integration';

@Directive({
  // eslint-disable-next-line @angular-eslint/directive-selector
  selector: 'a[href]'
})
export class ExternalLinkDirective implements OnChanges {
  private readonly integration = inject(INTEGRATION_SERVICE);

  @HostBinding('attr.rel')
  relAttr = '';

  @HostBinding('attr.target')
  targetAttr = '';

  @HostBinding('attr.href')
  hrefAttr = '';

  @Input()
  href: string = '';

  constructor(@Inject(PLATFORM_ID) private platformId: string) { }

  @HostListener('click', ['$event'])
  onClick(event: Event) {
    event.preventDefault();
    event.stopPropagation();

    this.integration.openExternal(this.href);
  }

  ngOnChanges() {
    this.hrefAttr = this.href;

    if (this.isLinkExternal()) {
      this.relAttr = 'noopener';
      this.targetAttr = '_blank';
    }
  }

  private isLinkExternal() {
    return (
      isPlatformBrowser(this.platformId) &&
      !this.href.includes(location.hostname)
    );
  }
}
