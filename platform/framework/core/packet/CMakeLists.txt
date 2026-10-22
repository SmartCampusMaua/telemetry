idf_component_register(

  SRCS
  "src/packet_builder.c"
  "src/packet_crc.c"
  "src/packet_validate.c"

  INCLUDE_DIRS
  "include"

  # src/ is private so packet_crc.h is visible only inside this component.
  PRIV_INCLUDE_DIRS
  "src"

  REQUIRES
  types
)
