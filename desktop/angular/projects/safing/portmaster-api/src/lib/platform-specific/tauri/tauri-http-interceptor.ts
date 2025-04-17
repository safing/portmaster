import { HttpEvent, HttpEventType, HttpHandlerFn, HttpRequest, HttpResponse, HttpHeaders, HttpErrorResponse } from '@angular/common/http';
import { from, Observable, switchMap, map, tap, catchError, throwError } from 'rxjs';
import { fetch } from '@tauri-apps/plugin-http';

/**
 * TauriHttpInterceptor intercepts HTTP requests and routes them through Tauri's `@tauri-apps/plugin-http` API.
 * 
 * This allows HTTP requests to be executed from the Tauri application binary instead of the WebView, 
 * enabling more secure and direct communication with external APIs.
 * 
 * The interceptor handles various response types (e.g., JSON, text, blob, arraybuffer) and ensures
 * that headers and response data are properly mapped to Angular's HttpResponse format.
 * 
 * References:
 * - https://v2.tauri.app/plugin/http-client/
 * - https://angular.dev/guide/http/interceptors 
 */
export function TauriHttpInterceptor(req: HttpRequest<unknown>, next: HttpHandlerFn): Observable<HttpEvent<unknown>> {
    const fetchOptions: RequestInit = {
        method: req.method,
        headers: req.headers.keys().reduce((acc: Record<string, string>, key) => {
            acc[key] = req.headers.get(key) || '';
            return acc;
        }, {}),
        body: req.body ? JSON.stringify(req.body) : undefined,
    };
    //console.log('[TauriHttpInterceptor] Fetching:', req.url, "Headers:", fetchOptions.headers);
    return from(fetch(req.url, fetchOptions)).pipe(
        switchMap(response => {
            // Copy all response headers
            const headerMap: Record<string, string> = {};                
            response.headers.forEach((value: string, key: string) => {
                headerMap[key] = value;
            });                
            const headers = new HttpHeaders(headerMap);

            // Check if response status is ok (2xx)
            if (!response.ok) {
                // Get the error content
                return from(response.text()).pipe(
                    map(errorText => {
                        throw new HttpErrorResponse({
                            error: errorText,
                            headers: headers,
                            status: response.status,
                            statusText: response.statusText,
                            url: req.url
                        });
                    })
                );
            }
            
            // Get the response type from the request
            const responseType = req.responseType || 'json';
                
            // Helper function to create HttpResponse from body
            const createResponse = (body: any): HttpEvent<unknown> => {
                return new HttpResponse({
                    body,
                    status: response.status,
                    headers: headers,
                    url: req.url
                }) as HttpEvent<unknown>;
            };
                
            switch (responseType) {
                case 'text':
                    return from(response.text()).pipe(map(createResponse));
                case 'arraybuffer':
                    return from(response.arrayBuffer()).pipe(map(createResponse));
                case 'blob':
                    return from(response.blob()).pipe(
                        map(blob => {
                            // Get content-type from response headers
                            const contentType = response.headers.get('content-type') || '';
                            // Create a new blob with the proper MIME type
                            if (contentType && (!blob.type || blob.type === 'application/octet-stream')) {
                                // Create new blob with the correct MIME type
                                const typedBlob = new Blob([blob], { type: contentType });
                                return createResponse(typedBlob);
                            }
                            
                            return createResponse(blob);
                        })
                    );
                case 'json':
                default:
                    return from(response.text()).pipe(
                        map(body => {
                            let parsedBody: any;
                            try {
                                // Only attempt to parse as JSON if we have content
                                // and either explicitly requested JSON or content-type is JSON
                                if (body && (responseType === 'json' || 
                                    (response.headers.get('content-type') || '').includes('application/json'))) {
                                    parsedBody = JSON.parse(body);
                                } else {
                                    parsedBody = body;
                                }
                            } catch (e) {
                                console.warn('[TauriHttpInterceptor] Failed to parse JSON response:', e);
                                parsedBody = body;
                            }
                            return createResponse(parsedBody);
                        })
                    );
            }
        }),
        catchError(error => {
            console.error('[TauriHttpInterceptor] Request failed:', error);
            
            // If it's already an HttpErrorResponse, just return it
            if (error instanceof HttpErrorResponse) {
                return throwError(() => error);
            }
            
            // Otherwise create a new HttpErrorResponse with available information
            return throwError(() => new HttpErrorResponse({
                error: error.message || 'Unknown error occurred',
                status: error.status || 0,
                statusText: error.statusText || 'Unknown Error',
                url: req.url,
                headers: error.headers ? new HttpHeaders(error.headers) : new HttpHeaders()
            }));
        })
    );
}

