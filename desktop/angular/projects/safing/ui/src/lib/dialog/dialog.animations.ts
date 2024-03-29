import { animate, state, style, transition, trigger } from "@angular/animations";

export const dialogAnimation = trigger(
  'dialogContainer',
  [
    state('void, exit', style({ opacity: 0, transform: 'scale(0.7)' })),
    state('enter', style({ transform: 'none', opacity: 1 })),
    transition(
      '* => enter',
      animate('120ms cubic-bezier(0, 0, 0.2, 1)',
        style({ opacity: 1, transform: 'translateY(0px)' }))
    ),
    transition(
      '* => void, * => exit',
      animate('120ms cubic-bezier(0, 0, 0.2, 1)',
        style({ opacity: 0, transform: 'scale(0.7)' }))
    ),
  ]
);
