//go:build linux && !jetkvm_native

package native

import "fmt"

func panicNativeDisabled() {
	panic("native backend disabled (build without -tags=jetkvm_native)")
}

func setUpNativeHandlers() {
	panicNativeDisabled()
}

func uiEventCodeToName(code int) string {
	panicNativeDisabled()
	return ""
}

func uiInit(rotation uint16) {
	panicNativeDisabled()
}

func uiTick() {
	panicNativeDisabled()
}

func videoInit(factor float64) error {
	return fmt.Errorf("native backend disabled (build without -tags=jetkvm_native)")
}

func videoShutdown() {
	panicNativeDisabled()
}

func videoStart() {
	panicNativeDisabled()
}

func videoStop() {
	panicNativeDisabled()
}

func videoGetStreamingStatus() VideoStreamingStatus {
	panicNativeDisabled()
	return VideoStreamingStatusInactive
}

func videoLogStatus() string {
	panicNativeDisabled()
	return ""
}

func uiSetVar(name string, value string) {
	panicNativeDisabled()
}

func uiGetVar(name string) string {
	panicNativeDisabled()
	return ""
}

func uiSwitchToScreen(screen string) {
	panicNativeDisabled()
}

func uiGetCurrentScreen() string {
	panicNativeDisabled()
	return ""
}

func uiObjAddState(objName string, state string) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func uiObjClearState(objName string, state string) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func uiGetLVGLVersion() string {
	panicNativeDisabled()
	return ""
}

func uiObjAddFlag(objName string, flag string) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func uiObjClearFlag(objName string, flag string) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func uiObjHide(objName string) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func uiObjShow(objName string) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func uiObjSetOpacity(objName string, opacity int) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func uiObjFadeIn(objName string, duration uint32) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func uiObjFadeOut(objName string, duration uint32) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func uiLabelSetText(objName string, text string) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func uiImgSetSrc(objName string, src string) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func uiDispSetRotation(rotation uint16) (bool, error) {
	panicNativeDisabled()
	return false, nil
}

func videoGetStreamQualityFactor() (float64, error) {
	panicNativeDisabled()
	return 0, nil
}

func videoSetStreamQualityFactor(factor float64) error {
	panicNativeDisabled()
	return nil
}

func videoGetEDID() (string, error) {
	panicNativeDisabled()
	return "", nil
}

func videoSetEDID(edid string) error {
	panicNativeDisabled()
	return nil
}

func crash() {
	panicNativeDisabled()
}

