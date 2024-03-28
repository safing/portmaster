import { AfterViewInit, Directive, ElementRef, HostBinding, Input, OnChanges, Renderer2, SimpleChanges } from '@angular/core';

@Directive({
  selector: 'span[appCountryFlags]',
})
export class CountryFlagDirective implements AfterViewInit, OnChanges {
  private readonly flagDir = "/assets/img/flags/";
  private readonly OFFSET = 127397;

  @HostBinding('style.text-shadow')
  textShadow = 'rgba(255, 255, 255, .5) 0px 0px 1px';

  @Input()
  appCountryFlags: string = '';

  constructor(
    private el: ElementRef,
    private renderer: Renderer2
  ) { }

  ngOnChanges(changes: SimpleChanges): void {
    if (!changes['appCountryFlags'].isFirstChange()) {
      this.update();
    }
  }

  ngAfterViewInit() {
    this.update();
  }

  private update() {
    const span = this.el.nativeElement as HTMLSpanElement;
    const flag = this.toUnicodeFlag(this.appCountryFlags);
    this.renderer.setAttribute(span, 'data-before', flag);

    span.innerHTML = `<img style="display: inline" src="${this.flagDir}${this.appCountryFlags.toLocaleUpperCase()}.png">`;
  }

  private toUnicodeFlag(code: string) {
    const base = 127462 - 65;
    const cc = code.toUpperCase();
    const res = String.fromCodePoint(...cc.split('').map(c => base + c.charCodeAt(0)));
    return res;
  }
}
