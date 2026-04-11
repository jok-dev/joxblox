package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	rl "github.com/gen2brain/raylib-go/raylib"
)

const shadowMapSize = 2048

var (
	loadedScene []loadedMeshModel

	renderTarget rl.RenderTexture2D
	renderWidth  int32
	renderHeight int32

	mainShader       rl.Shader
	mainShaderLoaded bool
	lightDirLoc      int32
	fillLightDirLoc  int32
	viewPosLoc       int32
	ambientLoc       int32
	lightColLoc      int32
	baseColLoc       int32
	lightVPLoc       int32
	shadowMapLoc     int32
	shadowBiasLoc    int32

	depthShader       rl.Shader
	depthShaderLoaded bool

	shadowMap       rl.RenderTexture2D
	shadowMapLoaded bool
)

type loadedMeshModel struct {
	model     rl.Model
	positions []float32
	normals   []float32
	indices   []uint16
	colors    []uint8
	baseColor [3]float32
	bounds    rl.BoundingBox
}

const mainVertSrc = `
#version 330
in vec3 vertexPosition;
in vec3 vertexNormal;
uniform mat4 mvp;
uniform mat4 matModel;
uniform mat3 matNormal;
uniform mat4 lightVP;
out vec3 fragPosition;
out vec3 fragNormal;
out vec4 fragPosLightSpace;
void main() {
    vec4 worldPos = matModel * vec4(vertexPosition, 1.0);
    fragPosition = worldPos.xyz;
    fragNormal = normalize(matNormal * vertexNormal);
    fragPosLightSpace = lightVP * worldPos;
    gl_Position = mvp * vec4(vertexPosition, 1.0);
}
`

const mainFragSrc = `
#version 330
in vec3 fragPosition;
in vec3 fragNormal;
in vec4 fragPosLightSpace;
uniform vec3 lightDir;
uniform vec3 fillLightDir;
uniform vec3 viewPos;
uniform vec3 ambientCol;
uniform vec3 lightCol;
uniform vec3 baseCol;
uniform sampler2D shadowMap;
uniform float shadowBias;
out vec4 finalColor;

float calcShadow(vec4 posLS) {
    vec3 proj = posLS.xyz / posLS.w;
    proj = proj * 0.5 + 0.5;
    if (proj.x < 0.0 || proj.x > 1.0 || proj.y < 0.0 || proj.y > 1.0 || proj.z > 1.0)
        return 0.0;
    float currentDepth = proj.z;
    float shadow = 0.0;
    vec2 texelSize = 1.0 / textureSize(shadowMap, 0);
    for (int x = -1; x <= 1; x++) {
        for (int y = -1; y <= 1; y++) {
            float closestDepth = texture(shadowMap, proj.xy + vec2(x, y) * texelSize).r;
            shadow += (currentDepth - shadowBias > closestDepth) ? 1.0 : 0.0;
        }
    }
    return shadow / 9.0;
}

void main() {
    vec3 norm = normalize(fragNormal);
    vec3 viewDir = normalize(viewPos - fragPosition);
    float diff = max(dot(norm, lightDir), 0.0);
    vec3 halfDir = normalize(lightDir + viewDir);
    float spec = pow(max(dot(norm, halfDir), 0.0), 32.0);
    vec3 key = diff * lightCol + spec * lightCol * 0.3;
    float fillDiff = max(dot(norm, fillLightDir), 0.0);
    vec3 fill = fillDiff * lightCol * 0.35;
    float rim = 1.0 - max(dot(norm, viewDir), 0.0);
    rim = pow(rim, 3.0) * 0.15;
    float shadow = calcShadow(fragPosLightSpace);
    vec3 lit = ambientCol + key * (1.0 - shadow * 0.65) + fill + rim;
    finalColor = vec4(lit * baseCol, 1.0);
}
`

const depthVertSrc = `
#version 330
in vec3 vertexPosition;
uniform mat4 mvp;
void main() {
    gl_Position = mvp * vec4(vertexPosition, 1.0);
}
`

const depthFragSrc = `
#version 330
out vec4 finalColor;
void main() {
    finalColor = vec4(vec3(gl_FragCoord.z), 1.0);
}
`

func main() {
	runtime.LockOSThread()

	rl.SetConfigFlags(rl.FlagWindowHidden | rl.FlagMsaa4xHint)
	rl.SetTraceLogLevel(rl.LogNone)
	rl.InitWindow(1, 1, "joxblox-mesh-renderer")
	defer rl.CloseWindow()

	mainShader = rl.LoadShaderFromMemory(mainVertSrc, mainFragSrc)
	mainShaderLoaded = true
	lightDirLoc = rl.GetShaderLocation(mainShader, "lightDir")
	fillLightDirLoc = rl.GetShaderLocation(mainShader, "fillLightDir")
	viewPosLoc = rl.GetShaderLocation(mainShader, "viewPos")
	ambientLoc = rl.GetShaderLocation(mainShader, "ambientCol")
	lightColLoc = rl.GetShaderLocation(mainShader, "lightCol")
	baseColLoc = rl.GetShaderLocation(mainShader, "baseCol")
	lightVPLoc = rl.GetShaderLocation(mainShader, "lightVP")
	shadowMapLoc = rl.GetShaderLocation(mainShader, "shadowMap")
	shadowBiasLoc = rl.GetShaderLocation(mainShader, "shadowBias")

	depthShader = rl.LoadShaderFromMemory(depthVertSrc, depthFragSrc)
	depthShaderLoaded = true

	shadowMap = rl.LoadRenderTexture(shadowMapSize, shadowMapSize)
	shadowMapLoaded = true

	stdinReader := bufio.NewReaderSize(os.Stdin, 16*1024*1024)

	for {
		line, readErr := stdinReader.ReadString('\n')
		if readErr != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		switch parts[0] {
		case "LOAD":
			handleLoad(parts, stdinReader, false)
		case "LOADC":
			handleLoad(parts, stdinReader, true)
		case "LOADSCENE":
			handleLoadScene(parts, stdinReader)
		case "RECOLOR":
			handleRecolor(parts, stdinReader)
		case "PICK":
			handlePick(parts)
		case "RENDER":
			handleRender(parts)
		case "QUIT":
			doCleanup()
			os.Exit(0)
		default:
			respond(fmt.Sprintf("ERR unknown command: %s", parts[0]))
		}
	}

	doCleanup()
}

func handleLoad(parts []string, reader io.Reader, hasColors bool) {
	vertexCount, indexCount, ok := parseLoadCounts(parts)
	if !ok {
		return
	}
	positions, indices32, colors, err := readMeshPayload(reader, vertexCount, indexCount, hasColors)
	if err != nil {
		respond(fmt.Sprintf("ERR read binary: %s", err.Error()))
		return
	}
	model, createErr := createModelFromRawData(positions, indices32, colors)
	if createErr != nil {
		respond(fmt.Sprintf("ERR %s", createErr.Error()))
		return
	}
	unloadLoadedScene()
	loadedScene = []loadedMeshModel{model}
	respond("OK")
}

func handleLoadScene(parts []string, reader *bufio.Reader) {
	if len(parts) < 2 {
		respond("ERR LOADSCENE requires batch_count")
		return
	}
	batchCount, err := strconv.Atoi(parts[1])
	if err != nil || batchCount <= 0 {
		respond("ERR invalid batch_count")
		return
	}

	nextScene := make([]loadedMeshModel, 0, batchCount)
	for batchIndex := 0; batchIndex < batchCount; batchIndex++ {
		headerLine, readErr := reader.ReadString('\n')
		if readErr != nil {
			unloadMeshModels(nextScene)
			respond(fmt.Sprintf("ERR read batch header: %s", readErr.Error()))
			return
		}
		batchParts := strings.Fields(strings.TrimSpace(headerLine))
		if len(batchParts) < 3 || batchParts[0] != "BATCH" {
			unloadMeshModels(nextScene)
			respond("ERR invalid BATCH header")
			return
		}
		vertexCount, err := strconv.Atoi(batchParts[1])
		if err != nil || vertexCount <= 0 {
			unloadMeshModels(nextScene)
			respond("ERR invalid batch vertex_count")
			return
		}
		indexCount, err := strconv.Atoi(batchParts[2])
		if err != nil || indexCount <= 0 {
			unloadMeshModels(nextScene)
			respond("ERR invalid batch index_count")
			return
		}

		positions, indices32, colors, payloadErr := readMeshPayload(reader, vertexCount, indexCount, true)
		if payloadErr != nil {
			unloadMeshModels(nextScene)
			respond(fmt.Sprintf("ERR read batch binary: %s", payloadErr.Error()))
			return
		}
		model, createErr := createModelFromRawData(positions, indices32, colors)
		if createErr != nil {
			unloadMeshModels(nextScene)
			respond(fmt.Sprintf("ERR %s", createErr.Error()))
			return
		}
		nextScene = append(nextScene, model)
	}

	unloadLoadedScene()
	loadedScene = nextScene
	respond("OK")
}

func handleRecolor(parts []string, reader io.Reader) {
	if len(parts) < 2 {
		respond("ERR RECOLOR requires batch_count")
		return
	}
	if len(loadedScene) == 0 {
		respond("ERR no scene loaded")
		return
	}
	batchCount, err := strconv.Atoi(parts[1])
	if err != nil || batchCount <= 0 {
		respond("ERR invalid batch_count")
		return
	}
	if batchCount != len(loadedScene) {
		respond("ERR recolor batch count mismatch")
		return
	}

	rawColors := make([]byte, batchCount*4)
	if _, readErr := io.ReadFull(reader, rawColors); readErr != nil {
		respond(fmt.Sprintf("ERR read recolor payload: %s", readErr.Error()))
		return
	}
	for batchIndex := 0; batchIndex < batchCount; batchIndex++ {
		offset := batchIndex * 4
		loadedScene[batchIndex].baseColor = [3]float32{
			float32(rawColors[offset]) / 255.0,
			float32(rawColors[offset+1]) / 255.0,
			float32(rawColors[offset+2]) / 255.0,
		}
	}
	respond("OK")
}

func parseLoadCounts(parts []string) (int, int, bool) {
	if len(parts) < 3 {
		respond("ERR LOAD requires vertex_count index_count")
		return 0, 0, false
	}
	vertexCount, err := strconv.Atoi(parts[1])
	if err != nil || vertexCount <= 0 {
		respond("ERR invalid vertex_count")
		return 0, 0, false
	}
	indexCount, err := strconv.Atoi(parts[2])
	if err != nil || indexCount <= 0 {
		respond("ERR invalid index_count")
		return 0, 0, false
	}
	return vertexCount, indexCount, true
}

func readMeshPayload(reader io.Reader, vertexCount int, indexCount int, hasColors bool) ([]float32, []uint32, []uint8, error) {
	posBytes := vertexCount * 3 * 4
	idxBytes := indexCount * 4
	colorBytes := 0
	if hasColors {
		colorBytes = vertexCount * 4
	}
	raw := make([]byte, posBytes+idxBytes+colorBytes)
	if _, readErr := io.ReadFull(reader, raw); readErr != nil {
		return nil, nil, nil, readErr
	}

	positions := make([]float32, vertexCount*3)
	for i := range positions {
		positions[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4 : i*4+4]))
	}
	indices32 := make([]uint32, indexCount)
	for i := range indices32 {
		offset := posBytes + i*4
		indices32[i] = binary.LittleEndian.Uint32(raw[offset : offset+4])
	}
	var colors []uint8
	if hasColors {
		colorOffset := posBytes + idxBytes
		colors = append([]uint8(nil), raw[colorOffset:colorOffset+colorBytes]...)
	}
	return positions, indices32, colors, nil
}

func createModelFromRawData(positions []float32, indices32 []uint32, colors []uint8) (loadedMeshModel, error) {
	vertexCount := len(positions) / 3
	if vertexCount <= 0 || len(indices32) < 3 {
		return loadedMeshModel{}, fmt.Errorf("mesh payload is empty")
	}
	if len(colors) == 0 {
		colors = buildDefaultColors(vertexCount)
	}
	if len(colors) != vertexCount*4 {
		return loadedMeshModel{}, fmt.Errorf("mesh colors length %d does not match vertex count %d", len(colors), vertexCount)
	}

	normals := computeVertexNormals(positions, indices32)
	indices16 := make([]uint16, len(indices32))
	for i, idx := range indices32 {
		if idx > 65535 {
			return loadedMeshModel{}, fmt.Errorf("mesh index %d exceeds uint16 range", idx)
		}
		indices16[i] = uint16(idx)
	}

	modelData := loadedMeshModel{
		positions: append([]float32(nil), positions...),
		normals:   normals,
		indices:   indices16,
		colors:    append([]uint8(nil), colors...),
		bounds:    computeBoundingBox(positions),
		baseColor: [3]float32{
			float32(colors[0]) / 255.0,
			float32(colors[1]) / 255.0,
			float32(colors[2]) / 255.0,
		},
	}
	mesh := rl.Mesh{
		VertexCount:   int32(vertexCount),
		TriangleCount: int32(len(indices16) / 3),
		Vertices:      &modelData.positions[0],
		Normals:       &modelData.normals[0],
		Indices:       &modelData.indices[0],
	}
	rl.UploadMesh(&mesh, false)
	modelData.model = rl.LoadModelFromMesh(mesh)
	return modelData, nil
}

func buildLightVP(lightDir rl.Vector3) rl.Matrix {
	lightPos := rl.Vector3{X: lightDir.X * 4, Y: lightDir.Y * 4, Z: lightDir.Z * 4}
	lightView := rl.MatrixLookAt(lightPos, rl.Vector3{}, rl.Vector3{X: 0, Y: 1, Z: 0})
	lightProj := rl.MatrixOrtho(-2.0, 2.0, -2.0, 2.0, 0.5, 10.0)
	return rl.MatrixMultiply(lightView, lightProj)
}

func buildCamera(cameraX float64, cameraY float64, cameraZ float64, yaw float64, pitch float64, zoom float64) rl.Camera3D {
	sinYaw := float32(math.Sin(yaw))
	cosYaw := float32(math.Cos(yaw))
	sinPitch := float32(math.Sin(pitch))
	cosPitch := float32(math.Cos(pitch))
	forwardX := -cosPitch * sinYaw
	forwardY := -sinPitch
	forwardZ := -cosPitch * cosYaw
	fovy := float32(math.Max(15.0, math.Min(90.0, 45.0/zoom)))

	return rl.Camera3D{
		Position:   rl.Vector3{X: float32(cameraX), Y: float32(cameraY), Z: float32(cameraZ)},
		Target:     rl.Vector3{X: float32(cameraX) + forwardX, Y: float32(cameraY) + forwardY, Z: float32(cameraZ) + forwardZ},
		Up:         rl.Vector3{X: 0, Y: 1, Z: 0},
		Fovy:       fovy,
		Projection: rl.CameraPerspective,
	}
}

func handlePick(parts []string) {
	if len(loadedScene) == 0 {
		respond("PICKED -1")
		return
	}
	if len(parts) < 11 {
		respond("ERR PICK requires width height cam_x cam_y cam_z yaw pitch zoom click_x click_y")
		return
	}

	width, _ := strconv.Atoi(parts[1])
	height, _ := strconv.Atoi(parts[2])
	cameraX, _ := strconv.ParseFloat(parts[3], 64)
	cameraY, _ := strconv.ParseFloat(parts[4], 64)
	cameraZ, _ := strconv.ParseFloat(parts[5], 64)
	yaw, _ := strconv.ParseFloat(parts[6], 64)
	pitch, _ := strconv.ParseFloat(parts[7], 64)
	zoom, _ := strconv.ParseFloat(parts[8], 64)
	clickX, _ := strconv.Atoi(parts[9])
	clickY, _ := strconv.Atoi(parts[10])

	if width < 1 || height < 1 {
		respond("PICKED -1")
		return
	}
	clickX = clampInt(clickX, 0, width-1)
	clickY = clampInt(clickY, 0, height-1)
	if zoom < 0.35 {
		zoom = 0.35
	}
	if zoom > 5.0 {
		zoom = 5.0
	}

	camera := buildCamera(cameraX, cameraY, cameraZ, yaw, pitch, zoom)
	ray := rl.GetScreenToWorldRayEx(
		rl.Vector2{X: float32(clickX), Y: float32(clickY)},
		camera,
		int32(width),
		int32(height),
	)

	bestBatch := -1
	bestDistance := float32(math.MaxFloat32)
	for batchIndex, meshModel := range loadedScene {
		boxCollision := rl.GetRayCollisionBox(ray, meshModel.bounds)
		if !boxCollision.Hit {
			continue
		}
		meshes := meshModel.model.GetMeshes()
		if len(meshes) == 0 {
			continue
		}
		meshCollision := rl.GetRayCollisionMesh(ray, meshes[0], meshModel.model.Transform)
		if !meshCollision.Hit || meshCollision.Distance < 0 {
			continue
		}
		if meshCollision.Distance < bestDistance {
			bestDistance = meshCollision.Distance
			bestBatch = batchIndex
		}
	}

	respond(fmt.Sprintf("PICKED %d", bestBatch))
}

func handleRender(parts []string) {
	if len(loadedScene) == 0 {
		respond("ERR no mesh loaded")
		return
	}
	if len(parts) < 12 {
		respond("ERR RENDER requires width height cam_x cam_y cam_z selected_batch yaw pitch zoom opacity bg_hex")
		return
	}

	width, _ := strconv.Atoi(parts[1])
	height, _ := strconv.Atoi(parts[2])
	cameraX, _ := strconv.ParseFloat(parts[3], 64)
	cameraY, _ := strconv.ParseFloat(parts[4], 64)
	cameraZ, _ := strconv.ParseFloat(parts[5], 64)
	selectedBatch, _ := strconv.Atoi(parts[6])
	yaw, _ := strconv.ParseFloat(parts[7], 64)
	pitch, _ := strconv.ParseFloat(parts[8], 64)
	zoom, _ := strconv.ParseFloat(parts[9], 64)
	opacity, _ := strconv.ParseFloat(parts[10], 64)
	bgHex := parts[11]

	if width < 1 || width > 4096 {
		width = 440
	}
	if height < 1 || height > 4096 {
		height = 300
	}
	if zoom < 0.35 {
		zoom = 0.35
	}
	if zoom > 5.0 {
		zoom = 5.0
	}
	opacity = clampFloat64(opacity, 0.1, 1.0)

	bgR, bgG, bgB := parseHexColor(bgHex)
	w, h := int32(width), int32(height)
	if renderWidth != w || renderHeight != h {
		if renderWidth > 0 {
			rl.UnloadRenderTexture(renderTarget)
		}
		renderTarget = rl.LoadRenderTexture(w, h)
		renderWidth = w
		renderHeight = h
	}

	camera := buildCamera(cameraX, cameraY, cameraZ, yaw, pitch, zoom)
	drawOrder := sceneDrawOrder(loadedScene, camera.Position, opacity)

	rl.BeginTextureMode(renderTarget)
	rl.ClearBackground(rl.Color{R: bgR, G: bgG, B: bgB, A: 255})
	rl.BeginMode3D(camera)
	for _, batchIndex := range drawOrder {
		meshModel := loadedScene[batchIndex]
		rl.DrawModel(meshModel.model, rl.Vector3{}, 1.0, modelTint(meshModel.baseColor, opacity))
		if batchIndex == selectedBatch {
			drawSelectedMeshHighlight(meshModel, bgR, bgG, bgB)
		}
	}
	rl.EndMode3D()
	rl.EndTextureMode()

	imageData := rl.LoadImageFromTexture(renderTarget.Texture)
	rl.ImageFlipVertical(imageData)
	byteCount := width * height * 4
	pixels := unsafe.Slice((*byte)(imageData.Data), byteCount)
	header := fmt.Sprintf("FRAME %d %d %d\n", width, height, byteCount)
	os.Stdout.WriteString(header)
	os.Stdout.Write(pixels)
	rl.UnloadImage(imageData)
}

func unloadLoadedScene() {
	unloadMeshModels(loadedScene)
	loadedScene = nil
}

func unloadMeshModels(models []loadedMeshModel) {
	for _, meshModel := range models {
		rl.UnloadModel(meshModel.model)
	}
}

func doCleanup() {
	unloadLoadedScene()
	if renderWidth > 0 {
		rl.UnloadRenderTexture(renderTarget)
		renderWidth = 0
	}
}

func respond(msg string) {
	os.Stdout.WriteString(msg + "\n")
}

func computeVertexNormals(positions []float32, indices []uint32) []float32 {
	vertexCount := len(positions) / 3
	normals := make([]float32, len(positions))
	for i := 0; i+2 < len(indices); i += 3 {
		a := int(indices[i])
		b := int(indices[i+1])
		c := int(indices[i+2])
		if a >= vertexCount || b >= vertexCount || c >= vertexCount {
			continue
		}
		ex := positions[b*3] - positions[a*3]
		ey := positions[b*3+1] - positions[a*3+1]
		ez := positions[b*3+2] - positions[a*3+2]
		fx := positions[c*3] - positions[a*3]
		fy := positions[c*3+1] - positions[a*3+1]
		fz := positions[c*3+2] - positions[a*3+2]
		nx := ey*fz - ez*fy
		ny := ez*fx - ex*fz
		nz := ex*fy - ey*fx
		for _, vertexIndex := range []int{a, b, c} {
			normals[vertexIndex*3] += nx
			normals[vertexIndex*3+1] += ny
			normals[vertexIndex*3+2] += nz
		}
	}
	for i := 0; i < vertexCount; i++ {
		x := normals[i*3]
		y := normals[i*3+1]
		z := normals[i*3+2]
		length := float32(math.Sqrt(float64(x*x + y*y + z*z)))
		if length > 0 {
			normals[i*3] /= length
			normals[i*3+1] /= length
			normals[i*3+2] /= length
		}
	}
	return normals
}

func buildDefaultColors(vertexCount int) []uint8 {
	if vertexCount <= 0 {
		return nil
	}
	colors := make([]uint8, vertexCount*4)
	for i := 0; i < vertexCount; i++ {
		base := i * 4
		colors[base+0] = 112
		colors[base+1] = 173
		colors[base+2] = 255
		colors[base+3] = 255
	}
	return colors
}

func modelTint(baseColor [3]float32, opacity float64) rl.Color {
	return rl.NewColor(
		uint8(clampColorFloat(baseColor[0])*255.0),
		uint8(clampColorFloat(baseColor[1])*255.0),
		uint8(clampColorFloat(baseColor[2])*255.0),
		uint8(clampFloat64(opacity, 0.1, 1.0)*255.0),
	)
}

func sceneDrawOrder(models []loadedMeshModel, cameraPos rl.Vector3, opacity float64) []int {
	order := make([]int, 0, len(models))
	for index := range models {
		order = append(order, index)
	}
	if opacity >= 0.999 {
		return order
	}
	sort.SliceStable(order, func(left int, right int) bool {
		leftDistance := modelDistanceSquared(models[order[left]], cameraPos)
		rightDistance := modelDistanceSquared(models[order[right]], cameraPos)
		return leftDistance > rightDistance
	})
	return order
}

func modelDistanceSquared(model loadedMeshModel, cameraPos rl.Vector3) float64 {
	centerX := float64(model.bounds.Min.X+model.bounds.Max.X) * 0.5
	centerY := float64(model.bounds.Min.Y+model.bounds.Max.Y) * 0.5
	centerZ := float64(model.bounds.Min.Z+model.bounds.Max.Z) * 0.5
	dx := centerX - float64(cameraPos.X)
	dy := centerY - float64(cameraPos.Y)
	dz := centerZ - float64(cameraPos.Z)
	return dx*dx + dy*dy + dz*dz
}

func computeBoundingBox(positions []float32) rl.BoundingBox {
	if len(positions) < 3 {
		return rl.BoundingBox{}
	}
	minX, minY, minZ := positions[0], positions[1], positions[2]
	maxX, maxY, maxZ := minX, minY, minZ
	for index := 3; index+2 < len(positions); index += 3 {
		x := positions[index]
		y := positions[index+1]
		z := positions[index+2]
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
		if z < minZ {
			minZ = z
		}
		if z > maxZ {
			maxZ = z
		}
	}
	return rl.BoundingBox{
		Min: rl.Vector3{X: minX, Y: minY, Z: minZ},
		Max: rl.Vector3{X: maxX, Y: maxY, Z: maxZ},
	}
}

func drawSelectedMeshHighlight(meshModel loadedMeshModel, bgR uint8, bgG uint8, bgB uint8) {
	outlineColor, shadowColor := selectedMeshOutlineColors(bgR, bgG, bgB)
	rl.DisableDepthTest()
	rl.SetLineWidth(4.0)
	rl.DrawModelWires(meshModel.model, rl.Vector3{}, 1.0, shadowColor)
	rl.SetLineWidth(2.0)
	rl.DrawModelWires(meshModel.model, rl.Vector3{}, 1.0, outlineColor)
	rl.EnableDepthTest()
	rl.SetLineWidth(1.0)
	box := expandedBoundingBox(meshModel.bounds, 0.02)
	rl.DrawBoundingBox(box, shadowColor)
	rl.DrawBoundingBox(expandedBoundingBox(meshModel.bounds, 0.01), outlineColor)
}

func expandedBoundingBox(bounds rl.BoundingBox, padding float32) rl.BoundingBox {
	min := bounds.Min
	max := bounds.Max
	sizeX := max.X - min.X
	sizeY := max.Y - min.Y
	sizeZ := max.Z - min.Z
	extra := padding
	if sizeX > 0 || sizeY > 0 || sizeZ > 0 {
		extra = float32(math.Max(float64(padding), float64(maxFloat32(sizeX, maxFloat32(sizeY, sizeZ))*0.03)))
	}
	return rl.BoundingBox{
		Min: rl.Vector3{X: min.X - extra, Y: min.Y - extra, Z: min.Z - extra},
		Max: rl.Vector3{X: max.X + extra, Y: max.Y + extra, Z: max.Z + extra},
	}
}

func selectedMeshOutlineColors(bgR uint8, bgG uint8, bgB uint8) (rl.Color, rl.Color) {
	luma := 0.2126*float64(bgR) + 0.7152*float64(bgG) + 0.0722*float64(bgB)
	if luma > 180 {
		return rl.NewColor(168, 32, 255, 255), rl.NewColor(24, 12, 32, 255)
	}
	return rl.NewColor(255, 64, 220, 255), rl.NewColor(8, 8, 8, 255)
}

func clampColorFloat(value float32) float32 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func clampFloat64(value float64, minimum float64, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func maxFloat32(left float32, right float32) float32 {
	if left > right {
		return left
	}
	return right
}

func clampInt(value int, minimum int, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func parseHexColor(hex string) (uint8, uint8, uint8) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 14, 17, 22
	}
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return uint8(r), uint8(g), uint8(b)
}
