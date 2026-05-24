export namespace webapp {
	
	export class AssetDetails {
	    assetId: number;
	    previewDataUrl: string;
	    statusMessage: string;
	    sourceDisplay: string;
	    stateDescription: string;
	    warningMessage: string;
	    assetTypeDisplay: string;
	    dimensionsLabel: string;
	    dimensionsDisplay: string;
	    durationDisplay: string;
	    selfSizeDisplay: string;
	    totalSizeDisplay: string;
	    formatDisplay: string;
	    contentTypeDisplay: string;
	    referencedDisplay: string;
	    referencedAssetIds: number[];
	    fileSha256Display: string;
	    assetDeliveryJson: string;
	    thumbnailJson: string;
	    economyJson: string;
	    rustyAssetToolJson: string;
	
	    static createFrom(source: any = {}) {
	        return new AssetDetails(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.assetId = source["assetId"];
	        this.previewDataUrl = source["previewDataUrl"];
	        this.statusMessage = source["statusMessage"];
	        this.sourceDisplay = source["sourceDisplay"];
	        this.stateDescription = source["stateDescription"];
	        this.warningMessage = source["warningMessage"];
	        this.assetTypeDisplay = source["assetTypeDisplay"];
	        this.dimensionsLabel = source["dimensionsLabel"];
	        this.dimensionsDisplay = source["dimensionsDisplay"];
	        this.durationDisplay = source["durationDisplay"];
	        this.selfSizeDisplay = source["selfSizeDisplay"];
	        this.totalSizeDisplay = source["totalSizeDisplay"];
	        this.formatDisplay = source["formatDisplay"];
	        this.contentTypeDisplay = source["contentTypeDisplay"];
	        this.referencedDisplay = source["referencedDisplay"];
	        this.referencedAssetIds = source["referencedAssetIds"];
	        this.fileSha256Display = source["fileSha256Display"];
	        this.assetDeliveryJson = source["assetDeliveryJson"];
	        this.thumbnailJson = source["thumbnailJson"];
	        this.economyJson = source["economyJson"];
	        this.rustyAssetToolJson = source["rustyAssetToolJson"];
	    }
	}
	export class AuthStatus {
	    enabled: boolean;
	    saved: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.saved = source["saved"];
	        this.message = source["message"];
	    }
	}
	export class ScanOptions {
	    pathFilter: string;
	    largeTextureThresholdMb: number;
	
	    static createFrom(source: any = {}) {
	        return new ScanOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pathFilter = source["pathFilter"];
	        this.largeTextureThresholdMb = source["largeTextureThresholdMb"];
	    }
	}
	export class ScanSummary {
	    totalAssets: number;
	    failedCount: number;
	    totalBytes: number;
	    totalBytesDisplay: string;
	    totalGpuBytes: number;
	    totalGpuDisplay: string;
	    totalTriangles: number;
	    largeTextureCount: number;
	
	    static createFrom(source: any = {}) {
	        return new ScanSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalAssets = source["totalAssets"];
	        this.failedCount = source["failedCount"];
	        this.totalBytes = source["totalBytes"];
	        this.totalBytesDisplay = source["totalBytesDisplay"];
	        this.totalGpuBytes = source["totalGpuBytes"];
	        this.totalGpuDisplay = source["totalGpuDisplay"];
	        this.totalTriangles = source["totalTriangles"];
	        this.largeTextureCount = source["largeTextureCount"];
	    }
	}
	export class ScanRow {
	    assetId: number;
	    assetInput: string;
	    useCount: number;
	    instanceType: string;
	    instancePath: string;
	    propertyName: string;
	    state: string;
	    source: string;
	    assetTypeName: string;
	    assetTypeId: number;
	    width: number;
	    height: number;
	    dimensionsDisplay: string;
	    format: string;
	    bytesSize: number;
	    totalBytesSize: number;
	    sizeDisplay: string;
	    gpuMemoryBytes: number;
	    gpuMemoryDisplay: string;
	    gpuFormatLabel: string;
	    meshTriangles: number;
	    warning: boolean;
	    warningCause: string;
	    isLarge: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ScanRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.assetId = source["assetId"];
	        this.assetInput = source["assetInput"];
	        this.useCount = source["useCount"];
	        this.instanceType = source["instanceType"];
	        this.instancePath = source["instancePath"];
	        this.propertyName = source["propertyName"];
	        this.state = source["state"];
	        this.source = source["source"];
	        this.assetTypeName = source["assetTypeName"];
	        this.assetTypeId = source["assetTypeId"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.dimensionsDisplay = source["dimensionsDisplay"];
	        this.format = source["format"];
	        this.bytesSize = source["bytesSize"];
	        this.totalBytesSize = source["totalBytesSize"];
	        this.sizeDisplay = source["sizeDisplay"];
	        this.gpuMemoryBytes = source["gpuMemoryBytes"];
	        this.gpuMemoryDisplay = source["gpuMemoryDisplay"];
	        this.gpuFormatLabel = source["gpuFormatLabel"];
	        this.meshTriangles = source["meshTriangles"];
	        this.warning = source["warning"];
	        this.warningCause = source["warningCause"];
	        this.isLarge = source["isLarge"];
	    }
	}
	export class ScanReport {
	    filePath: string;
	    rows: ScanRow[];
	    summary: ScanSummary;
	
	    static createFrom(source: any = {}) {
	        return new ScanReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePath = source["filePath"];
	        this.rows = this.convertValues(source["rows"], ScanRow);
	        this.summary = this.convertValues(source["summary"], ScanSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class Settings {
	    cacheEnabled: boolean;
	    cacheFolder: string;
	    studioPath: string;
	    meshLookSensitivity: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cacheEnabled = source["cacheEnabled"];
	        this.cacheFolder = source["cacheFolder"];
	        this.studioPath = source["studioPath"];
	        this.meshLookSensitivity = source["meshLookSensitivity"];
	    }
	}

}

