
import { Pipe, PipeTransform } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { DomSanitizer, SafeUrl } from '@angular/platform-browser';
import { Observable, of } from 'rxjs';
import { map, catchError, tap } from 'rxjs/operators';

//**
// * This pipe fetches an image from a given URL and returns a SafeUrl for use in Angular templates.
// * It caches the image to avoid multiple requests for the same URL.
// * If the image fails to load, it returns null and logs an error message.
// * 
// * The pipe uses Angular's HttpClient to make the HTTP request and DomSanitizer to sanitize the URL.
// * 
// * This pipe is useful for forcing usage of the HTTP interceptor in Tauri instead of the WebView 
// * (for example, to use the Tauri API for HTTP requests)
// *
// * @example
// * <img [src]="imageUrl | httpImgSrc | async" />
// **/

@Pipe({ 
    name: 'httpImgSrc'
})
export class HttpImgSrcPipe implements PipeTransform {
    private static cache = new Map<string, SafeUrl | null>();

    constructor(private http: HttpClient, private sanitizer: DomSanitizer) { }

    transform(url: string | SafeUrl | undefined): Observable<SafeUrl | null> {
        if (!url) {
            return of(null);
        }

        if (this.isSafeUrl(url)) {
            //console.log('[Pipe httpImgSrc]  URL is already a SafeUrl:', url);
            return of(url);
        }

        if (typeof url !== 'string') {
            //console.error('[Pipe httpImgSrc]  Invalid URL:', url);
            return of(null);
        }

        if (HttpImgSrcPipe.cache.has(url)) {
            //console.log('[Pipe httpImgSrc]  Returning cached image:', url);
            return of(HttpImgSrcPipe.cache.get(url) as SafeUrl | null);
        }

        return this.http.get(url, { responseType: 'blob' }).pipe(
            //tap(blob => console.log('[Pipe httpImgSrc] Successfully loaded image:', url, 'Size:', blob.size, 'Type:', blob.type)),
            map(blob => URL.createObjectURL(blob)),
            map(objectUrl => {
                const safeUrl = this.sanitizer.bypassSecurityTrustUrl(objectUrl);

                if (HttpImgSrcPipe.cache.size > 1000) {
                    // Very simple cache eviction strategy: clear the cache if it exceeds 1000 items. 
                    // Normally it should never exceed this size, but just in case.
                    // TODO: Implement a more sophisticated cache eviction strategy if needed.
                    console.warn('[Pipe httpImgSrc] Cache size exceeded 1000 items. Clearing images cache.');
                    HttpImgSrcPipe.cache.clear();
                }

                HttpImgSrcPipe.cache.set(url, safeUrl);
                return safeUrl;
            }),
            catchError(() => {
                console.error('[Pipe httpImgSrc]  Failed to load image:', url);
                HttpImgSrcPipe.cache.set(url, null);
                return of(null);
            })
        );
    }
    // Type guard for SafeUrl
    private isSafeUrl(value: any): value is SafeUrl {
        // SafeUrl is an object with an internal property changing per Angular versions
        // But it’s always object-like, not a string, and created by DomSanitizer
        return typeof value === 'object' && value !== null && value.changingThisBreaksApplicationSecurity !== undefined;
    }
}
