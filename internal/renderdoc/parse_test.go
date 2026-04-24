package renderdoc

import (
	"strings"
	"testing"
)

const fixtureTwoTextures = `<?xml version="1.0"?>
<rdc>
	<header>
		<driver id="1">D3D11</driver>
	</header>
	<chunks version="20">
		<chunk name="Internal::Driver Initialisation Parameters">
			<struct name="InitParams" typename="D3D11InitParams">
				<struct name="AdapterDesc" typename="DXGI_ADAPTER_DESC">
					<string name="Description" typename="string">NVIDIA GeForce RTX 3080</string>
				</struct>
			</struct>
		</chunk>
		<chunk name="ID3D11Device::CreateTexture2D">
			<struct name="Descriptor" typename="D3D11_TEXTURE2D_DESC">
				<uint name="Width" typename="uint32_t" width="4">1024</uint>
				<uint name="Height" typename="uint32_t" width="4">1024</uint>
				<uint name="MipLevels" typename="uint32_t" width="4">11</uint>
				<uint name="ArraySize" typename="uint32_t" width="4">1</uint>
				<enum name="Format" typename="DXGI_FORMAT" width="4" string="DXGI_FORMAT_BC3_UNORM">77</enum>
				<enum name="Usage" typename="D3D11_USAGE" width="4" string="D3D11_USAGE_DEFAULT">0</enum>
				<enum name="BindFlags" typename="D3D11_BIND_FLAG" width="4" string="D3D11_BIND_SHADER_RESOURCE">8</enum>
				<enum name="CPUAccessFlags" typename="D3D11_CPU_ACCESS_FLAG" width="4" string="0">0</enum>
				<enum name="MiscFlags" typename="D3D11_RESOURCE_MISC_FLAG" width="4" string="0">0</enum>
			</struct>
			<ResourceId name="pTexture" typename="ID3D11Texture2D *" width="8">12345</ResourceId>
		</chunk>
		<chunk name="ID3D11Device::CreateTexture2D">
			<struct name="Descriptor" typename="D3D11_TEXTURE2D_DESC">
				<uint name="Width" typename="uint32_t" width="4">2048</uint>
				<uint name="Height" typename="uint32_t" width="4">2048</uint>
				<uint name="MipLevels" typename="uint32_t" width="4">1</uint>
				<uint name="ArraySize" typename="uint32_t" width="4">1</uint>
				<enum name="Format" typename="DXGI_FORMAT" width="4" string="DXGI_FORMAT_R16G16B16A16_FLOAT">10</enum>
				<enum name="Usage" typename="D3D11_USAGE" width="4" string="D3D11_USAGE_DEFAULT">0</enum>
				<enum name="BindFlags" typename="D3D11_BIND_FLAG" width="4" string="D3D11_BIND_RENDER_TARGET | D3D11_BIND_SHADER_RESOURCE">40</enum>
				<enum name="MiscFlags" typename="D3D11_RESOURCE_MISC_FLAG" width="4" string="0">0</enum>
			</struct>
			<ResourceId name="pTexture" typename="ID3D11Texture2D *" width="8">67890</ResourceId>
		</chunk>
	</chunks>
</rdc>`

func TestParseCaptureXMLExtractsTextures(t *testing.T) {
	report, err := parseCaptureXML(strings.NewReader(fixtureTwoTextures))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if report.GPUAdapter != "NVIDIA GeForce RTX 3080" {
		t.Errorf("expected adapter name, got %q", report.GPUAdapter)
	}
	if len(report.Textures) != 2 {
		t.Fatalf("expected 2 textures, got %d", len(report.Textures))
	}

	bcTexture := report.Textures[0]
	if bcTexture.ResourceID != "12345" {
		t.Errorf("bc resource id = %q", bcTexture.ResourceID)
	}
	if bcTexture.Width != 1024 || bcTexture.Height != 1024 || bcTexture.MipLevels != 11 {
		t.Errorf("bc dims: %dx%d mips=%d", bcTexture.Width, bcTexture.Height, bcTexture.MipLevels)
	}
	if bcTexture.ShortFormat != "BC3_UNORM" {
		t.Errorf("bc short format = %q", bcTexture.ShortFormat)
	}
	if bcTexture.Category != CategoryAssetAlpha {
		t.Errorf("bc category = %q", bcTexture.Category)
	}
	// BC3 1024x1024 with full mip chain: 1.33 MB (~1398101 bytes)
	if bcTexture.Bytes < 1_390_000 || bcTexture.Bytes > 1_405_000 {
		t.Errorf("bc bytes = %d; expected ~1.33MB", bcTexture.Bytes)
	}

	rtTexture := report.Textures[1]
	if rtTexture.Category != CategoryRenderTgt {
		t.Errorf("rt category = %q", rtTexture.Category)
	}
	// R16G16B16A16_FLOAT 2048x2048 no mips: 8 B/px * 2048 * 2048 = 32 MB
	expectedRT := int64(8 * 2048 * 2048)
	if rtTexture.Bytes != expectedRT {
		t.Errorf("rt bytes = %d; expected %d", rtTexture.Bytes, expectedRT)
	}

	if report.TotalBytes != bcTexture.Bytes+rtTexture.Bytes {
		t.Errorf("total bytes = %d; expected %d", report.TotalBytes, bcTexture.Bytes+rtTexture.Bytes)
	}

	bcAgg := report.ByCategory[CategoryAssetAlpha]
	if bcAgg.Count != 1 || bcAgg.Bytes != bcTexture.Bytes {
		t.Errorf("bc category agg = %+v", bcAgg)
	}
}

func TestComputeTextureBytesBC1MatchesExpected(t *testing.T) {
	bytes := computeTextureBytes(1024, 1024, 1, 1, "DXGI_FORMAT_BC1_UNORM", 0.5)
	// BC1: 0.5 B/px * 1024 * 1024 = 524288 bytes base level
	if bytes != 524288 {
		t.Errorf("BC1 1024² base = %d; expected 524288", bytes)
	}
}

func TestComputeTextureBytesRespectsFullMipChain(t *testing.T) {
	bytes := computeTextureBytes(1024, 1024, 0, 1, "DXGI_FORMAT_BC3_UNORM", 1.0)
	// Full chain multiplier is ~1.333x base (11 mips at 1024²)
	baseLevel := int64(1024 * 1024)
	if bytes < baseLevel || bytes > int64(float64(baseLevel)*1.4) {
		t.Errorf("BC3 1024² full-chain = %d; expected ~1.33x of %d", bytes, baseLevel)
	}
}

func TestCategorizeUsesMiscFlagsForCubemap(t *testing.T) {
	info := &TextureInfo{
		Width:      512,
		Height:     512,
		Format:     "DXGI_FORMAT_R8G8B8A8_UNORM",
		BindFlags:  "D3D11_BIND_SHADER_RESOURCE",
		MiscFlags:  "D3D11_RESOURCE_MISC_TEXTURECUBE",
		IsBCFormat: false,
	}
	if got := categorize(info); got != CategoryCubemap {
		t.Errorf("cubemap category = %q", got)
	}
}
