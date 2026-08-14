//go:build darwin

package nativesurface

/*
#cgo CFLAGS: -x objective-c -fblocks -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa -framework WebKit

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include <stdlib.h>

typedef struct {
    int action;
    void *native;
    const char *url;
    int navigate;
    double x;
    double y;
    double width;
    double height;
    int visible;
    double alpha;
} NativeSurfaceOperation;

typedef struct {
    void *native;
    double x;
    double y;
    double width;
    double height;
} NativeSurfaceResult;

static NSRect surfaceRect(NSView *contentView, NativeSurfaceOperation operation) {
    CGFloat y = NSHeight(contentView.bounds) - operation.y - operation.height;
    return NSMakeRect(operation.x, y, operation.width, operation.height);
}

static int applyNativeSurfaceBatch(
    void *windowPointer,
    NativeSurfaceOperation *operations,
    int operationCount,
    NativeSurfaceResult *results,
    int *resultCount
) {
    if (windowPointer == NULL) return 1;
    __block int status = 0;
    dispatch_block_t apply = ^{
        NSWindow *window = (NSWindow *)windowPointer;
        NSView *contentView = window.contentView;
        if (contentView == nil) { status = 2; return; }

        // Create every replacement before mutating the installed inventory.
        // A failed allocation therefore leaves the previous inventory intact.
        for (int index = 0; index < operationCount; index++) {
            NativeSurfaceOperation *operation = &operations[index];
            if (operation->action != 1) continue;
            WKWebViewConfiguration *configuration = [[WKWebViewConfiguration alloc] init];
            WKWebView *browser = [[WKWebView alloc] initWithFrame:surfaceRect(contentView, *operation) configuration:configuration];
            [configuration release];
            if (browser == nil) { status = 3; break; }
            operation->native = browser;
        }
        if (status != 0) {
            for (int index = 0; index < operationCount; index++) {
                NativeSurfaceOperation *operation = &operations[index];
                if (operation->action == 1 && operation->native != NULL) {
                    [(WKWebView *)operation->native release];
                    operation->native = NULL;
                }
            }
            return;
        }

        for (int index = 0; index < operationCount; index++) {
            NativeSurfaceOperation operation = operations[index];
            if (operation.action == 3) {
                WKWebView *browser = (WKWebView *)operation.native;
                [browser stopLoading];
                [browser removeFromSuperview];
                [browser release];
                continue;
            }
            WKWebView *browser = (WKWebView *)operation.native;
            browser.autoresizingMask = NSViewNotSizable;
            browser.frame = surfaceRect(contentView, operation);
            browser.hidden = operation.visible == 0;
            browser.alphaValue = operation.alpha;
            if ((operation.action == 1 || operation.navigate != 0) && operation.url != NULL) {
                NSString *urlString = [NSString stringWithUTF8String:operation.url];
                NSURL *url = [NSURL URLWithString:urlString];
                if (url != nil) [browser loadRequest:[NSURLRequest requestWithURL:url]];
            }
        }

        // Non-remove operations arrive in declared layer order. Repositioning
        // each installed child establishes that exact order in the same commit.
        int output = 0;
        for (int index = 0; index < operationCount; index++) {
            NativeSurfaceOperation operation = operations[index];
            if (operation.action == 3) continue;
            WKWebView *browser = (WKWebView *)operation.native;
            [contentView addSubview:browser positioned:NSWindowAbove relativeTo:nil];
            NSRect frame = browser.frame;
            results[output++] = (NativeSurfaceResult){
                .native = browser,
                .x = frame.origin.x,
                .y = NSHeight(contentView.bounds) - NSMaxY(frame),
                .width = frame.size.width,
                .height = frame.size.height,
            };
        }
        *resultCount = output;
    };
    if ([NSThread isMainThread]) apply(); else dispatch_sync(dispatch_get_main_queue(), apply);
    return status;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type appKitBatchDriver struct{}

// NewNativeBackend returns the reusable macOS native-surface writer. Wails
// supplies only its NSWindow pointer through Service; AppKit ownership remains
// entirely inside this package.
func NewNativeBackend() Backend {
	return newNativeBackend(appKitBatchDriver{})
}

func (appKitBatchDriver) apply(window unsafe.Pointer, operations []nativeOperation) ([]nativeResult, error) {
	if len(operations) == 0 {
		return []nativeResult{}, nil
	}
	cOperations := make([]C.NativeSurfaceOperation, len(operations))
	urls := make([]*C.char, len(operations))
	defer func() {
		for _, url := range urls {
			if url != nil {
				C.free(unsafe.Pointer(url))
			}
		}
	}()
	for index, operation := range operations {
		if operation.action != nativeRemove {
			urls[index] = C.CString(operation.surface.Source.URL)
		}
		cOperations[index] = C.NativeSurfaceOperation{
			action: C.int(operation.action), native: operation.native, url: urls[index],
			navigate: C.int(boolInt(operation.navigate)),
			x:        C.double(operation.surface.Frame.X), y: C.double(operation.surface.Frame.Y),
			width: C.double(operation.surface.Frame.Width), height: C.double(operation.surface.Frame.Height),
			visible: C.int(boolInt(operation.surface.Visible)), alpha: C.double(operation.surface.Alpha),
		}
	}
	cResults := make([]C.NativeSurfaceResult, len(operations))
	var resultCount C.int
	status := C.applyNativeSurfaceBatch(
		window, &cOperations[0], C.int(len(cOperations)),
		&cResults[0], &resultCount,
	)
	if status != 0 {
		return nil, fmt.Errorf("apply native AppKit surface batch: status=%d", int(status))
	}

	results := make([]nativeResult, 0, int(resultCount))
	output := 0
	for _, operation := range operations {
		if operation.action == nativeRemove {
			continue
		}
		result := cResults[output]
		output++
		surface := operation.surface
		surface.Frame = Frame{X: float64(result.x), Y: float64(result.y), Width: float64(result.width), Height: float64(result.height)}
		results = append(results, nativeResult{surface: surface, native: result.native})
	}
	return results, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
