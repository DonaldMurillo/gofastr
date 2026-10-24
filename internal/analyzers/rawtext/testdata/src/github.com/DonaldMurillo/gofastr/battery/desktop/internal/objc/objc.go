package objc

type ID uintptr

func NSString(s string) ID { return 0 }

func NSStringErr(s string) (ID, error) { return 0, nil }
