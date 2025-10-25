package netguard

// GetTrafficStats 获取流量统计信息（用于外部访问）
func GetTrafficStats() []*TrafficRecord {
	var stats []*TrafficRecord
	trafficMap.Range(func(key, value interface{}) bool {
		if record, ok := value.(*TrafficRecord); ok {
			// 创建副本避免并发问题
			record.RLock()
			stat := &TrafficRecord{
				LocalIP:       record.LocalIP,
				LocalPort:     record.LocalPort,
				RemoteIP:      record.RemoteIP,
				RemotePort:    record.RemotePort,
				Protocol:      record.Protocol,
				ProcessName:   record.ProcessName,
				ProcessPID:    record.ProcessPID,
				BytesSent:     record.BytesSent,
				BytesReceived: record.BytesReceived,
				LastUpdate:    record.LastUpdate,
			}
			record.RUnlock()
			stats = append(stats, stat)
		}
		return true
	})
	return stats
}
