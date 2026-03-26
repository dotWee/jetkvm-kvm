//go:build linux && !cgo

package native

func panicCGODisabled() {
	panic("cgo disabled")
}

func setUpNativeHandlers() {
	panicCGODisabled()
}

func uiSetVar(name string, value string) {
	panicCGODisabled()
}

func uiGetVar(name string) string {
	panicCGODisabled()
	return ""
}

func uiSwitchToScreen(screen string) {
	panicCGODisabled()
}

func uiGetCurrentScreen() string {
	panicCGODisabled()
	return ""
}

func uiObjAddState(objName string, state string) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiObjClearState(objName string, state string) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiObjAddFlag(objName string, flag string) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiObjClearFlag(objName string, flag string) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiObjHide(objName string) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiObjShow(objName string) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiObjSetOpacity(objName string, opacity int) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiObjFadeIn(objName string, duration uint32) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiObjFadeOut(objName string, duration uint32) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiLabelSetText(objName string, text string) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiImgSetSrc(objName string, src string) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiDispSetRotation(rotation uint16) (bool, error) {
	panicCGODisabled()
	return false, nil
}

func uiEventCodeToName(code int) string {
	panicCGODisabled()
	return ""
}

func uiGetLVGLVersion() string {
	panicCGODisabled()
	return ""
}

func videoGetStreamQualityFactor() (float64, error) {
	panicCGODisabled()
	return 0, nil
}

func videoSetStreamQualityFactor(factor float64) error {
	panicCGODisabled()
	return nil
}

func videoLogStatus() string {
	panicCGODisabled()
	return ""
}

func videoGetEDID() (string, error) {
	panicCGODisabled()
	return "", nil
}

func videoSetEDID(edid string) error {
	panicCGODisabled()
	return nil
}

func videoGetStreamingStatus() VideoStreamingStatus {
	panicCGODisabled()
	return VideoStreamingStatusInactive
}

func crash() {
	panicCGODisabled()
}

