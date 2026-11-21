export namespace config {
	
	export class Config {
	    downloadDir: string;
	    stateDir: string;
	    maxConcurrentDownloads: number;
	    maxConnectionsPerDownload: number;
	    minSegmentSize: number;
	    chunkSize: number;
	    globalSpeedLimit: number;
	    retryMaxAttempts: number;
	    retryInitialDelayMs: number;
	    retryMaxDelayMS: number;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.downloadDir = source["downloadDir"];
	        this.stateDir = source["stateDir"];
	        this.maxConcurrentDownloads = source["maxConcurrentDownloads"];
	        this.maxConnectionsPerDownload = source["maxConnectionsPerDownload"];
	        this.minSegmentSize = source["minSegmentSize"];
	        this.chunkSize = source["chunkSize"];
	        this.globalSpeedLimit = source["globalSpeedLimit"];
	        this.retryMaxAttempts = source["retryMaxAttempts"];
	        this.retryInitialDelayMs = source["retryInitialDelayMs"];
	        this.retryMaxDelayMS = source["retryMaxDelayMS"];
	    }
	}

}

export namespace model {
	
	export class Checksum {
	    algo: string;
	    hex: string;
	
	    static createFrom(source: any = {}) {
	        return new Checksum(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.algo = source["algo"];
	        this.hex = source["hex"];
	    }
	}
	export class Segment {
	    index: number;
	    start: number;
	    end: number;
	    downloaded: number;
	    done: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Segment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.downloaded = source["downloaded"];
	        this.done = source["done"];
	    }
	}
	export class Download {
	    id: string;
	    url: string;
	    name: string;
	    finalPath: string;
	    partPath: string;
	    totalSize: number;
	    downloaded: number;
	    etag: string;
	    lastModified: string;
	    rangeOk: boolean;
	    probed: boolean;
	    status: string;
	    segments: Segment[];
	    checksum?: Checksum;
	    error?: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Download(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	        this.name = source["name"];
	        this.finalPath = source["finalPath"];
	        this.partPath = source["partPath"];
	        this.totalSize = source["totalSize"];
	        this.downloaded = source["downloaded"];
	        this.etag = source["etag"];
	        this.lastModified = source["lastModified"];
	        this.rangeOk = source["rangeOk"];
	        this.probed = source["probed"];
	        this.status = source["status"];
	        this.segments = this.convertValues(source["segments"], Segment);
	        this.checksum = this.convertValues(source["checksum"], Checksum);
	        this.error = source["error"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
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

}

