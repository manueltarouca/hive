# JNI: the native library calls back into nothing, but its entry points are
# looked up by name.
-keepclasseswithmembernames class * { native <methods>; }
