local rawIds = [[
rbxassetid://73285238905269
rbxassetid://110084209792907
rbxassetid://103047846381223
rbxassetid://113280860848462
rbxassetid://134066447953518
rbxassetid://122392010586339
rbxassetid://90846428648278
rbxassetid://109632926407204
rbxassetid://134118204908292
rbxassetid://80465578056125
rbxassetid://104613765560588
rbxassetid://95231566240000
rbxassetid://119096592029684
rbxassetid://133726932381394
rbxassetid://89594968330451
rbxassetid://139910299846676
rbxassetid://127776479441539
rbxassetid://96790233204035
rbxassetid://91831352415839
rbxassetid://118107530211845
rbxassetid://80090468263175
rbxassetid://105060170458307
rbxassetid://72629179298333
rbxassetid://127894617886322
rbxassetid://98271747530543
rbxassetid://83779214907460
rbxassetid://87233890455134
rbxassetid://135606125896306
rbxassetid://112079566165483
rbxassetid://133949219646551
rbxassetid://137183192346974
rbxassetid://103391337758208
rbxassetid://98700815330027
rbxassetid://135907702560743
rbxassetid://105734135259632
rbxassetid://99012646146686
rbxassetid://132871561416934
rbxassetid://128571952635351
rbxassetid://77781588436286
rbxassetid://89958531019118
rbxassetid://133811644221682
rbxassetid://103967425433550
rbxassetid://128332369992195
rbxassetid://129782755217234
rbxassetid://125882878633595
rbxassetid://101790143449723
rbxassetid://77899619407575
rbxassetid://125008223314728
rbxassetid://74976402767932
rbxassetid://121401057545975
]]

-- Parse IDs
local ids = {}
for line in rawIds:gmatch("[^\r\n]+") do
	line = line:match("^%s*(.-)%s*$")
	if line ~= "" then
		table.insert(ids, line)
	end
end
local baseTextureCount = 25
local totalCubeCount = 50
local normalMapIds = {}
local roughnessMapIds = {}
for i = 1, math.min(baseTextureCount, #ids) do
	table.insert(normalMapIds, ids[i])
end
for i = baseTextureCount + 1, math.min(baseTextureCount*2, #ids) do
	table.insert(roughnessMapIds, ids[i])
end

local fixedColorMap = "rbxassetid://129796843836895"

-- Grid settings (X/Y plane)
local partSize = Vector3.new(7, 7, 7)
local padding = 1
local columns = math.ceil(math.sqrt(totalCubeCount))
local rows = math.ceil(totalCubeCount / columns)

local stepX = partSize.X + padding
local stepY = partSize.Y + padding
local fixedZ = 0

-- Container
local model = Instance.new("Model")
model.Name = "MeshParts_SurfaceAppearance_NormalMapWall"
model.Parent = workspace

-- Center around origin
local totalWidth = (columns - 1) * stepX
local totalHeight = (rows - 1) * stepY
local startX = -totalWidth / 2
local startY = totalHeight / 2 + partSize.Y / 2

for i = 1, totalCubeCount do
	local normalMapId = normalMapIds[((i - 1) % #normalMapIds) + 1]
	local roughnessMapId = roughnessMapIds[((i - 1) % #roughnessMapIds) + 1]
	local idx = i - 1
	local col = idx % columns
	local row = math.floor(idx / columns)

	local x = startX + col * stepX
	local y = startY - row * stepY

	local meshPart = Instance.new("MeshPart")
	meshPart.Name = ("TextureMeshPart_%03d"):format(i)
	meshPart.Size = partSize
	meshPart.Anchored = true
	meshPart.CanCollide = false
	meshPart.Position = Vector3.new(x, y, fixedZ)
	meshPart.Orientation = Vector3.new(0, 0, 90) -- requested
	meshPart.Parent = model

	local surfaceAppearance = Instance.new("SurfaceAppearance")
	surfaceAppearance.Name = "SurfaceAppearance"
	surfaceAppearance.ColorMap = normalMapId
	surfaceAppearance.NormalMap = roughnessMapId
	surfaceAppearance.Parent = meshPart
end

print(("Created %d anchored MeshParts using first %d IDs for NormalMap and next %d IDs for RoughnessMap (both repeated)."):format(totalCubeCount, #normalMapIds, #roughnessMapIds))