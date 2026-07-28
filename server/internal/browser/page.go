package browser

import (
	"errors"
	"time"

	"breckr-server/internal/types"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// rodPage adapts a go-rod page to types.Page.
//
// Everything the executor needs is expressed in terms of a selector, so this is
// the only file that knows go-rod exists. Swapping CDP clients again would be a
// change here and nowhere else.
type rodPage struct {
	page *rod.Page
}

// Navigate loads the URL and waits for DOMContentLoaded.
//
// Deliberately not the load event: the previous implementation used puppeteer's
// `waitUntil: "domcontentloaded"`, and waiting for full load instead would stall
// on any page with a slow image or a hanging analytics beacon -- pages a monitor
// still needs to read.
//
// The wait is bounded and its expiry is *not* an error. Lightpanda implements
// only part of CDP, and a server that never emits Page.lifecycleEvent would
// otherwise hang here until the whole run timed out. Readiness is really
// enforced downstream -- the selector wait for text/number/attribute, and the
// run timeout for everything -- so falling through after the bound is the
// honest behavior rather than a silent failure.
func (p *rodPage) Navigate(url string) error {
	wait := p.page.
		Timeout(types.NavigationTimeout).
		WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)

	if err := p.page.Navigate(url); err != nil {
		return err
	}

	wait()
	return nil
}

func (p *rodPage) WaitForSelector(selector string, timeout time.Duration) error {
	_, err := p.page.Timeout(timeout).Element(selector)
	return err
}

// Exists reports whether the selector matches right now, without waiting.
//
// rod.NotFoundSleeper is what makes it not wait: the default sleeper retries
// until the page's timeout, which for an "alert me when this appears" check
// would burn the whole budget on the common case of "not there yet".
func (p *rodPage) Exists(selector string) (bool, error) {
	_, err := p.page.Sleeper(rod.NotFoundSleeper).Element(selector)
	if err == nil {
		return true, nil
	}

	var notFound *rod.ElementNotFoundError
	if errors.As(err, &notFound) {
		return false, nil
	}
	return false, err
}

// Count evaluates the length in the page rather than materializing the matches.
//
// go-rod's Elements() unwraps the querySelectorAll result as a CDP array, and
// Lightpanda returns it as a NodeList object instead -- which go-rod rejects
// outright. Asking the page for the number is both portable and cheaper: a
// count of 500 elements costs one round trip instead of 500 remote handles.
func (p *rodPage) Count(selector string) (int, error) {
	result, err := p.page.Eval(`(selector) => document.querySelectorAll(selector).length`, selector)
	if err != nil {
		return 0, err
	}
	return result.Value.Int(), nil
}

func (p *rodPage) Attribute(selector, name string) (string, error) {
	element, err := p.page.Element(selector)
	if err != nil {
		return "", err
	}

	value, err := element.Attribute(name)
	if err != nil {
		return "", err
	}
	// A missing attribute reads as an empty string, matching the previous
	// `getAttribute(name) ?? ""`.
	if value == nil {
		return "", nil
	}
	return *value, nil
}

func (p *rodPage) Text(selector string) (string, error) {
	element, err := p.page.Element(selector)
	if err != nil {
		return "", err
	}
	return element.Text()
}

var _ types.Page = (*rodPage)(nil)
